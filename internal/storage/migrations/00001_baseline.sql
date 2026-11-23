-- Boshlang'ich sxema: goose'gacha gorm AutoMigrate yaratgan jadvallar
-- (Session, User, Order, Payment, Visit) aynan takrorlanadi — ustun turlari,
-- default qiymatlar, NOT NULL, birlamchi kalitlar va GORM nomlari bilan
-- indekslar. Bo'sh bazada AutoMigrate'dan keyin olingan
-- pg_dump --schema-only natijasidan normallashtirilgan.
--
-- Production bazada bu jadvallar allaqachon mavjud, shuning uchun har bir
-- buyruq IF NOT EXISTS bilan yozilgan va u yerda hech narsani o'zgartirmaydi.
-- Mavjud obyektlar hech qachon DROP yoki ALTER qilinmaydi; shu sababli
-- "Down" bo'limi ham yo'q.

-- +goose Up

CREATE TABLE IF NOT EXISTS sessions (
    user_id bigint NOT NULL,
    chat_id bigint NOT NULL,
    username varchar(64),
    state varchar(32) NOT NULL DEFAULT 'idle',
    work_type varchar(32),
    topic varchar(512),
    volume bigint NOT NULL DEFAULT 0,
    design varchar(32),
    tariff varchar(16),
    language varchar(16),
    requirements varchar(1024),
    order_id bigint,
    student_name varchar(128),
    university varchar(256),
    teacher_name varchar(128),
    plan varchar(16),
    generated_count bigint NOT NULL DEFAULT 0,
    last_generated_at timestamptz,
    guide_sent_at timestamptz,
    created_at timestamptz,
    updated_at timestamptz,
    PRIMARY KEY (user_id)
);
CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions (state);

CREATE TABLE IF NOT EXISTS users (
    id bigserial,
    telegram_user_id bigint NOT NULL,
    username varchar(64),
    first_name varchar(128),
    is_admin boolean NOT NULL DEFAULT false,
    admin_since timestamptz,
    created_at timestamptz,
    updated_at timestamptz,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_telegram_user_id ON users (telegram_user_id);
CREATE INDEX IF NOT EXISTS idx_users_is_admin ON users (is_admin);

CREATE TABLE IF NOT EXISTS orders (
    id bigserial,
    user_id bigint NOT NULL,
    telegram_user_id bigint NOT NULL,
    work_type varchar(32) NOT NULL,
    topic varchar(512),
    volume bigint NOT NULL DEFAULT 0,
    design varchar(32),
    language varchar(16),
    requirements varchar(1024),
    student_name varchar(128),
    university varchar(256),
    teacher_name varchar(128),
    kind varchar(16) NOT NULL,
    price bigint NOT NULL DEFAULT 0,
    is_free boolean NOT NULL DEFAULT false,
    is_paid boolean NOT NULL DEFAULT false,
    status varchar(16) NOT NULL DEFAULT 'pending_payment',
    paid_at timestamptz,
    delivered_at timestamptz,
    created_at timestamptz,
    updated_at timestamptz,
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_orders_user_status ON orders (user_id, status);
CREATE INDEX IF NOT EXISTS idx_orders_telegram_user_id ON orders (telegram_user_id);

CREATE TABLE IF NOT EXISTS payments (
    id bigserial,
    user_id bigint NOT NULL,
    telegram_user_id bigint NOT NULL,
    username varchar(64),
    order_id bigint NOT NULL,
    kind varchar(16) NOT NULL,
    amount bigint NOT NULL,
    detected_amount bigint NOT NULL DEFAULT 0,
    detected_last4 varchar(8),
    detected_paid_at timestamptz,
    transaction_id varchar(128),
    status varchar(16) NOT NULL,
    reason varchar(1024),
    ai_raw text,
    check_file_id varchar(256) NOT NULL,
    check_file_unique_id varchar(128),
    check_file_type varchar(16) NOT NULL DEFAULT 'photo',
    check_message_id bigint NOT NULL,
    reviewed_by bigint,
    reviewed_at timestamptz,
    created_at timestamptz,
    updated_at timestamptz,
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_payments_user_id ON payments (user_id);
CREATE INDEX IF NOT EXISTS idx_payments_telegram_user_id ON payments (telegram_user_id);
CREATE INDEX IF NOT EXISTS idx_payments_order_id ON payments (order_id);
CREATE INDEX IF NOT EXISTS idx_payments_transaction_id ON payments (transaction_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments (status);
CREATE INDEX IF NOT EXISTS idx_payments_check_file_unique_id ON payments (check_file_unique_id);
CREATE INDEX IF NOT EXISTS idx_payments_created_at ON payments (created_at);

CREATE TABLE IF NOT EXISTS visits (
    day date NOT NULL,
    user_id bigint NOT NULL,
    PRIMARY KEY (day, user_id)
);
