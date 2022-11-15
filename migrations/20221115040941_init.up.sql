CREATE TYPE order_status AS ENUM ('pending', 'success', 'failed');

CREATE TABLE reward_payout (
    id BIGSERIAL not null,
    order_id uuid not null default public.uuid_generate_v4(),
    status order_status,
    sc_id uuid unique not null,
    PRIMARY KEY (order_id)
);