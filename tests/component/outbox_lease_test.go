//go:build component

package component

import (
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/parker"
)

func TestOutboxReclaimsExpiredLease(t *testing.T) {
	const payload = "test-event"
	fixture := newActivityFixture(t)
	id, originalToken := uuid.NewString(), uuid.NewString()
	_, err := fixture.pool.Exec(fixture.ctx, `
		INSERT INTO outbox (id, topic, message_key, message_value, status, created_at, claim_token, lease_until)
		VALUES ($1, $2, $3, $4, 'publishing', '2000-01-01', $5, now() - interval '1 second')
	`, id, fixture.material, id, []byte(payload), originalToken)
	if err != nil {
		t.Fatal(err)
	}

	store := parker.NewPgOutbox(fixture.pool)
	records, err := store.FetchPending(fixture.ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ID != id || records[0].ClaimToken == originalToken ||
		records[0].Key != id || string(records[0].Value) != payload {
		t.Fatal("reclaim must keep the original message and issue a new token")
	}
	record := records[0]
	stale := record
	stale.ClaimToken = originalToken
	if err = store.MarkClaimSent(fixture.ctx, stale); err == nil {
		t.Fatal("expired lease acknowledged a message")
	}
	if err = store.MarkClaimSent(fixture.ctx, record); err != nil {
		t.Fatal(err)
	}
	var sent bool
	err = fixture.pool.QueryRow(fixture.ctx, `
		SELECT status = 'sent' AND claim_token IS NULL AND lease_until IS NULL
		FROM outbox WHERE id = $1
	`, id).Scan(&sent)
	if err != nil || !sent {
		t.Fatal("acknowledgement did not release the claim", err)
	}
}
