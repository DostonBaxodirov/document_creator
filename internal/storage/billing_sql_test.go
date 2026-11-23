package storage

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Bazasiz testlar: so'rovlar DryRun rejimida faqat SQL ga aylantiriladi
// (ulanish ochilmaydi), shuning uchun TEST_DATABASE_URL talab qilinmaydi.

// dryRunBilling SQL ni bajarmasdan buf ga yozib boradigan repozitoriy
// qaytaradi. queryFields bo'lsa, GORM `SELECT *` o'rniga ustunlarni
// nomma-nom yozadi — shunda qaysi ustun o'qilishi SQL matnida ko'rinadi.
func dryRunBilling(t *testing.T, queryFields bool) (*BillingRepository, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=test dbname=test sslmode=disable"}),
		&gorm.Config{
			DisableAutomaticPing: true,
			Logger:               gormlogger.New(log.New(buf, "", 0), gormlogger.Config{LogLevel: gormlogger.Info}),
		})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close(db) })
	return NewBillingRepository(db.Session(&gorm.Session{DryRun: true, QueryFields: queryFields})), buf
}

// TestListPaymentsOmitsAIRaw — ro'yxat so'rovlari katta ai_raw ustunini
// o'qimaydi, GetPayment esa barcha ustunlarni o'qiydi.
func TestListPaymentsOmitsAIRaw(t *testing.T) {
	ctx := context.Background()
	since := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	calls := []struct {
		name string
		list bool // ro'yxat so'rovi — ai_raw o'qilmasligi kerak
		run  func(r *BillingRepository)
	}{
		{"ListPayments", true, func(r *BillingRepository) { _, _ = r.ListPayments(ctx, nil) }},
		{"ListPayments(since)", true, func(r *BillingRepository) { _, _ = r.ListPayments(ctx, &since) }},
		{"ListPaymentsByStatus", true, func(r *BillingRepository) { _, _, _ = r.ListPaymentsByStatus(ctx, PaymentReview, 5) }},
		{"ListPaymentsByStatus(limitsiz)", true, func(r *BillingRepository) { _, _, _ = r.ListPaymentsByStatus(ctx, PaymentReview, 0) }},
		{"GetPayment", false, func(r *BillingRepository) { _, _ = r.GetPayment(ctx, 1) }},
	}

	for _, queryFields := range []bool{false, true} {
		r, buf := dryRunBilling(t, queryFields)
		for _, c := range calls {
			buf.Reset()
			c.run(r)
			sql := buf.String()
			if !strings.Contains(sql, `FROM "payments"`) {
				t.Fatalf("%s (QueryFields=%v): SQL yozib olinmadi: %q", c.name, queryFields, sql)
			}

			if c.list {
				// `SELECT *` ham ai_raw ni o'qiydi, shuning uchun ustunlar
				// ro'yxati aniq yozilgan bo'lishi kerak.
				if strings.Contains(sql, "ai_raw") || strings.Contains(sql, "SELECT *") ||
					!strings.Contains(sql, "check_file_id") {
					t.Errorf("%s (QueryFields=%v): ai_raw o'qilmasligi kerak:\n%s", c.name, queryFields, sql)
				}
				continue
			}
			// GetPayment barcha ustunlarni o'qiydi: QueryFields bilan ai_raw
			// nomma-nom, usiz — `SELECT *` orqali.
			want := "SELECT *"
			if queryFields {
				want = "ai_raw"
			}
			if !strings.Contains(sql, want) {
				t.Errorf("%s (QueryFields=%v): SQL da %q bo'lishi kerak:\n%s", c.name, queryFields, want, sql)
			}
		}
	}
}
