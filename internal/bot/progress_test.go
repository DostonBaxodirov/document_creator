package bot

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

// testBoard — test uchun jarayon xabari (test oxirida yopiladi).
func testBoard(t *testing.T, b *tele.Bot) *progressBoard {
	t.Helper()
	pb := newProgressBoard(context.Background(), b, &tele.Chat{ID: 1}, zap.NewNop(), progressInterval)
	t.Cleanup(pb.Close)
	return pb
}

// progressUpdates — jarayon xabarini yuborish va tahrirlash so'rovlari.
func progressUpdates(calls []apiCall) []apiCall {
	var out []apiCall
	for _, c := range calls {
		if c.method == "sendMessage" || c.method == "editMessageText" {
			out = append(out, c)
		}
	}
	return out
}

// waitProgress — jarayon xabarida want matni paydo bo'lguncha kutadi.
func waitProgress(t *testing.T, calls func() []apiCall, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		updates := progressUpdates(calls())
		for _, c := range updates {
			if c.params["text"] == want {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("jarayon xabarida %q ko'rinmadi: %+v", want, updates)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestProgressBoardConcurrentStages — ikki bosqich bir vaqtda bitta xabarda
// ko'rsatiladi: bitta sendMessage, keyin editMessageText lar va oxirida
// bitta deleteMessage.
func TestProgressBoardConcurrentStages(t *testing.T) {
	b, calls := fakeTelegram(t, "")
	pb := newProgressBoard(context.Background(), b, &tele.Chat{ID: 7}, zap.NewNop(), 20*time.Millisecond)

	reportText, doneText := pb.Stage(textStage("Bo'limlar yozilmoqda..."))
	reportImg, doneImg := pb.Stage(imageStage)

	// Ikkala bosqich parallel ishlaydi.
	var wg sync.WaitGroup
	wg.Go(func() { reportText(0, 8); reportText(3, 8) })
	wg.Go(func() { reportImg(0, 3); reportImg(1, 3) })
	wg.Wait()
	waitProgress(t, calls, "✍️ <b>Bo'limlar yozilmoqda...</b> ▰▰▰▱▱▱▱▱▱▱ 3/8\n"+
		"🖼 <b>Rasmlar chizilmoqda...</b> ▰▰▰▱▱▱▱▱▱▱ 1/3")

	// Tugagan bosqich xabardan olib tashlanadi.
	doneText()
	reportImg(2, 3)
	const imagesOnly = "🖼 <b>Rasmlar chizilmoqda...</b> ▰▰▰▰▰▰▱▱▱▱ 2/3"
	waitProgress(t, calls, imagesOnly)

	doneImg()
	pb.Close()
	pb.Close() // takroriy chaqiruv xavfsiz

	got := calls()
	if len(got) < 3 {
		t.Fatalf("kamida sendMessage, editMessageText va deleteMessage kutilgan: %+v", got)
	}
	if got[0].method != "sendMessage" || fmt.Sprint(got[0].params["chat_id"]) != "7" {
		t.Fatalf("birinchi so'rov chatga sendMessage bo'lishi kerak: %+v", got[0])
	}
	last := got[len(got)-1]
	if last.method != "deleteMessage" {
		t.Fatalf("oxirgi so'rov deleteMessage bo'lishi kerak: %+v", last)
	}
	for _, c := range got[1 : len(got)-1] {
		if c.method != "editMessageText" {
			t.Errorf("o'rtada faqat editMessageText bo'lishi kerak: %+v", c)
		}
	}
	for _, c := range got[1:] {
		if id := fmt.Sprint(c.params["message_id"]); id != "101" {
			t.Errorf("%s yuborilgan xabarning o'ziga (101) tegishli bo'lishi kerak: %s", c.method, id)
		}
	}
	// Faol bosqich qolmaganda xabar tahrirlanmaydi (bo'sh matn yuborilmaydi).
	if text := got[len(got)-2].params["text"]; text != imagesOnly {
		t.Errorf("o'chirishdan oldingi oxirgi matn %q, kutilgan %q", text, imagesOnly)
	}
}

// TestProgressBoardThrottlesEdits — holat tez-tez o'zgarsa ham Telegram'ga
// interval da bir martadan ko'p murojaat qilinmaydi.
func TestProgressBoardThrottlesEdits(t *testing.T) {
	const interval = 100 * time.Millisecond
	b, calls := fakeTelegram(t, "")
	pb := newProgressBoard(context.Background(), b, &tele.Chat{ID: 7}, zap.NewNop(), interval)
	report, done := pb.Stage(textStage("Slaydlar yozilmoqda..."))

	started := time.Now()
	reports := 0
	for time.Since(started) < 700*time.Millisecond {
		reports++
		report(reports, 1000) // har safar yangi matn
		time.Sleep(2 * time.Millisecond)
	}
	done()
	pb.Close()
	elapsed := time.Since(started)

	updates := progressUpdates(calls())
	if len(updates) < 2 {
		t.Fatalf("xabar yangilanib borishi kerak edi: %+v", updates)
	}
	if limit := int(elapsed/interval) + 1; len(updates) > limit {
		t.Errorf("%v ichida ko'pi bilan %d ta yangilanish kutilgan, bo'ldi %d (%d ta holatdan)", elapsed, limit, len(updates), reports)
	}
	for k := 1; k < len(updates); k++ {
		if gap := updates[k].at.Sub(updates[k-1].at); gap < interval {
			t.Errorf("%d-yangilanish oldingisidan %v keyin keldi (kamida %v kerak)", k, gap, interval)
		}
	}
	if n := countMethod(calls(), "sendMessage"); n != 1 {
		t.Errorf("xabar bir marta yuborilishi kerak, sendMessage=%d", n)
	}
}

// TestProgressBoardConcurrentReports — report 50 ta goroutine'dan bir vaqtda
// chaqiriladi (-race bilan tekshiriladi) va Telegram javobini kutmaydi.
func TestProgressBoardConcurrentReports(t *testing.T) {
	b, calls := fakeTelegram(t, "")
	// Birinchi xabardan keyin goroutine bir soat kutadi — report baribir
	// bloklanmasligi kerak.
	pb := newProgressBoard(context.Background(), b, &tele.Chat{ID: 7}, zap.NewNop(), time.Hour)
	var (
		reports [2]func(done, total int)
		dones   [2]func()
	)
	for i, st := range []progressStage{imageStage, textStage("Bo'limlar yozilmoqda...")} {
		reports[i], dones[i] = pb.Stage(st)
	}

	var wg sync.WaitGroup
	for g := range 50 {
		wg.Go(func() {
			for i := range 200 {
				reports[g%2](i, 200)
			}
		})
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("report bloklanib qoldi")
	}

	deadline := time.Now().Add(3 * time.Second)
	for countMethod(calls(), "sendMessage") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("jarayon xabari yuborilmadi")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, done := range dones {
		done()
		done() // takroriy chaqiruv xavfsiz
	}
	pb.Close()

	got := calls()
	if s, e, d := countMethod(got, "sendMessage"), countMethod(got, "editMessageText"), countMethod(got, "deleteMessage"); s != 1 || e != 0 || d != 1 {
		t.Errorf("sendMessage=%d, editMessageText=%d, deleteMessage=%d; kutilgan 1, 0, 1", s, e, d)
	}
}

// TestProgressBoardStopsOnCancel — ish konteksti bekor qilinsa (timeout yoki
// /cancel), xabar Close kutilmasdan o'chiriladi.
func TestProgressBoardStopsOnCancel(t *testing.T) {
	b, calls := fakeTelegram(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	pb := newProgressBoard(ctx, b, &tele.Chat{ID: 7}, zap.NewNop(), time.Hour)
	report, _ := pb.Stage(designStage)
	report(0, 1)
	waitProgress(t, calls, progressText(0, 1, designStage))

	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for countMethod(calls(), "deleteMessage") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("bekor qilingan ishda jarayon xabari o'chirilmadi")
		}
		time.Sleep(5 * time.Millisecond)
	}
	report(1, 1) // to'xtagandan keyin ham bloklanmaydi
	pb.Close()
	if n := countMethod(calls(), "deleteMessage"); n != 1 {
		t.Errorf("xabar bir marta o'chirilishi kerak, deleteMessage=%d", n)
	}
}
