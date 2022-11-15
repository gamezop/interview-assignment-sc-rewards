-- name: GetRewardPayoutByOrderId :one
SELECT * FROM reward_payout
WHERE order_id = $1 LIMIT 1;

-- name: GetRewardPayoutByScratchId :one
SELECT * FROM reward_payout
WHERE sc_id = $1 LIMIT 1;

-- name: CreateRewardPayout :one
INSERT INTO reward_payout (
    status, sc_id
) VALUES (
    $1, $2
)
RETURNING *;

-- name: UpdateRewardPayoutStatus :exec
UPDATE reward_payout 
SET status = $1
WHERE order_id = $2;

