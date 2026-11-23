package storage

import (
	"context"
	"errors"
	"testing"
)

func TestBroadcastLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	r := NewBroadcastRepository(db)

	if last, err := r.Last(ctx, BroadcastReferral); err != nil || last != nil {
		t.Fatalf("Last bo'sh bazada: %+v %v", last, err)
	}
	b, err := r.Start(ctx, BroadcastReferral, 7, 3)
	if err != nil || b.ID == 0 || b.Status != BroadcastRunning {
		t.Fatalf("Start: %+v %v", b, err)
	}
	// Ikkinchi e'lon birinchisi tugaguncha boshlanmaydi.
	if _, err := r.Start(ctx, BroadcastReferral, 8, 3); !errors.Is(err, ErrConflict) {
		t.Errorf("ikkinchi Start: err = %v, kutilgan ErrConflict", err)
	}

	if err := r.SetMessage(ctx, b.ID, 55); err != nil {
		t.Fatal(err)
	}
	b.Sent, b.Blocked, b.LastUserID = 2, 1, 30
	if err := r.SaveProgress(ctx, b); err != nil {
		t.Fatal(err)
	}
	running, err := r.Running(ctx)
	if err != nil || len(running) != 1 {
		t.Fatalf("Running: %+v %v", running, err)
	}
	if got := running[0]; got.MessageID != 55 || got.Sent != 2 || got.Blocked != 1 || got.LastUserID != 30 || got.Processed() != 3 {
		t.Errorf("saqlangan holat: %+v", got)
	}

	if err := r.Finish(ctx, b, BroadcastDone); err != nil {
		t.Fatal(err)
	}
	if running, _ := r.Running(ctx); len(running) != 0 {
		t.Errorf("yakunlangan e'lon hali running: %+v", running)
	}
	last, err := r.Last(ctx, BroadcastReferral)
	if err != nil || last == nil || last.ID != b.ID || last.Status != BroadcastDone || last.FinishedAt == nil {
		t.Errorf("Last: %+v %v", last, err)
	}
	// Yakunlangandan keyin yangi e'lon boshlash mumkin.
	if _, err := r.Start(ctx, BroadcastReferral, 7, 3); err != nil {
		t.Errorf("yangi Start: %v", err)
	}
}

func TestBroadcastRecipients(t *testing.T) {
	ctx := context.Background()
	testDB(t)
	r := NewBroadcastRepository(sharedDB)
	for _, id := range []int64{30, 10, 20, 40} {
		mkChat(t, id, "")
	}
	if n, err := r.CountRecipients(ctx); err != nil || n != 4 {
		t.Fatalf("CountRecipients = %d, %v", n, err)
	}
	var got []int64
	after := int64(0)
	for {
		page, err := r.Recipients(ctx, after, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, p := range page {
			if p.ChatID != p.UserID {
				t.Errorf("chat_id = %d, kutilgan %d", p.ChatID, p.UserID)
			}
			got = append(got, p.UserID)
		}
		after = page[len(page)-1].UserID
	}
	want := []int64{10, 20, 30, 40}
	if len(got) != len(want) {
		t.Fatalf("oluvchilar = %v, kutilgan %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("oluvchilar = %v, kutilgan %v", got, want)
		}
	}
}

func TestReferralTotals(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	br := NewBillingRepository(db)
	mkChat(t, 1, "ali")
	if _, err := br.AttachReferral(ctx, 1, 2, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := br.AttachReferral(ctx, 1, 3, "", ""); err != nil {
		t.Fatal(err)
	}
	referee := mkUser(t, br, 2)
	if _, err := br.RewardReferral(ctx, referee.ID, 1); err != nil {
		t.Fatal(err)
	}
	u := mkUser(t, br, 1)
	setCredits(t, br, u, 1)
	if _, err := br.CreateBonusOrder(ctx, bonusOrder(u, 0, 3000)); err != nil {
		t.Fatal(err)
	}
	got, err := NewStatsRepository(db, nil).ReferralTotals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != (ReferralTotals{Joined: 2, Rewarded: 1, BonusOrders: 1}) {
		t.Errorf("ReferralTotals = %+v", got)
	}
}
