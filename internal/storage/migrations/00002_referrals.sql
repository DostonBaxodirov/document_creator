-- Do'stlarni taklif qilish (referal) tizimi va admin e'lonlari:
--   users.free_credits  — taklif bonuslari (bepul imkoniyatlar) balansi;
--   orders.discount     — buyurtmada bonus qoplagan summa;
--   referrals           — kim kimni taklif qilgani va mukofot berilgani;
--   broadcasts          — barcha foydalanuvchilarga yuborilayotgan e'lonlar
--                         (bot qayta ishga tushsa, kursordan davom etadi).
--
-- Sxema GORM AutoMigrate yaratadigan ko'rinishni aynan takrorlaydi
-- (TestMigrateMatchesAutoMigrate): yangi ustunlar jadval oxiriga qo'shiladi,
-- indeks nomlari GORM nomlari bilan. Buyruqlar IF NOT EXISTS bilan yozilgan.

-- +goose Up

ALTER TABLE users ADD COLUMN IF NOT EXISTS free_credits bigint NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS discount bigint NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS referrals (
    id bigserial,
    referrer_user_id bigint NOT NULL,
    referee_user_id bigint NOT NULL,
    created_at timestamptz,
    rewarded_at timestamptz,
    reward_order_id bigint,
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_referrals_referrer_user_id ON referrals (referrer_user_id);
-- Bitta foydalanuvchini faqat bir kishi taklif qilgan bo'lishi mumkin.
CREATE UNIQUE INDEX IF NOT EXISTS idx_referrals_referee_user_id ON referrals (referee_user_id);

CREATE TABLE IF NOT EXISTS broadcasts (
    id bigserial,
    kind varchar(32) NOT NULL,
    status varchar(16) NOT NULL,
    started_by bigint NOT NULL,
    message_id bigint NOT NULL DEFAULT 0,
    total bigint NOT NULL DEFAULT 0,
    sent bigint NOT NULL DEFAULT 0,
    blocked bigint NOT NULL DEFAULT 0,
    failed bigint NOT NULL DEFAULT 0,
    last_user_id bigint NOT NULL DEFAULT 0,
    created_at timestamptz,
    finished_at timestamptz,
    PRIMARY KEY (id)
);
-- Bir turdagi e'londan bir vaqtda faqat bittasi yuborilishi mumkin.
CREATE UNIQUE INDEX IF NOT EXISTS idx_broadcasts_running ON broadcasts (kind) WHERE status = 'running';

-- +goose Down

DROP TABLE IF EXISTS broadcasts;
DROP TABLE IF EXISTS referrals;
ALTER TABLE orders DROP COLUMN IF EXISTS discount;
ALTER TABLE users DROP COLUMN IF EXISTS free_credits;
