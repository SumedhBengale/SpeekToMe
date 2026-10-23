package db

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func vocabularyDB(t *testing.T) *Database {
	t.Helper()
	d, err := NewDatabase(filepath.Join(t.TempDir(), "vocabulary.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.db.SetMaxOpenConns(1)
	if _, err = d.db.Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active) VALUES(1,'vocabulary@example.test','Test','free',0)`); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestVocabularyLimitConcurrentAddsAndIdempotency(t *testing.T) {
	d := vocabularyDB(t)
	for i := 0; i < 19; i++ {
		if _, err := d.AddVocabularyTerm(1, fmt.Sprintf("term%02d", i), "term"); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := d.AddVocabularyTerm(1, fmt.Sprintf("concurrent%d", i), "term")
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrVocabularyLimit) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected one remaining slot, got %d successful additions", successes)
	}
	if _, err := d.AddVocabularyTerm(1, "term00", "updated"); err != nil {
		t.Fatal("idempotent write at limit:", err)
	}
	saved, err := d.GetVocabularyTerms(1)
	if err != nil || len(saved) != 20 {
		t.Fatal(len(saved), err)
	}
}

func TestVocabularyDowngradePreservesAndUpgradeRestores(t *testing.T) {
	d := vocabularyDB(t)
	if _, err := d.db.Exec(`UPDATE users SET plan_tier='pro',plan_active=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000) INSERT INTO vocabulary(user_id,canonical_term,normalized_term) SELECT 1,printf('term%04d',x),printf('term%04d',x) FROM n`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AddVocabularyTerm(1, "extra", "extra"); !errors.Is(err, ErrVocabularyLimit) {
		t.Fatal("Pro cap bypassed:", err)
	}
	for _, tc := range []struct {
		tier   string
		active int
		end    int64
		want   int
	}{
		{"pro", 1, 0, 1000}, {"free", 0, 0, 20}, {"pro", 0, 0, 20},
		{"pro", 1, time.Now().Add(-SubscriptionGracePeriod - time.Hour).Unix(), 20}, {"pro", 1, time.Now().Add(time.Hour).Unix(), 1000},
	} {
		if _, err := d.db.Exec(`UPDATE users SET plan_tier=?,plan_active=?,razorpay_current_end=? WHERE id=1`, tc.tier, tc.active, tc.end); err != nil {
			t.Fatal(err)
		}
		active, err := d.GetActiveVocabularyTerms(1)
		if err != nil || len(active) != tc.want {
			t.Fatalf("%+v: active=%d error=%v", tc, len(active), err)
		}
		if active[0].Canonical != "term0001" {
			t.Fatal("active selection must remain alphabetical")
		}
		saved, err := d.GetVocabularyTerms(1)
		if err != nil || len(saved) != 1000 {
			t.Fatal("downgrade deleted saved terms", len(saved), err)
		}
		if _, err = d.AddVocabularyTerm(1, "extra", "extra"); !errors.Is(err, ErrVocabularyLimit) {
			t.Fatal("cap bypassed:", err)
		}
		if _, err = d.AddVocabularyTerm(1, "term0001", "updated"); err != nil {
			t.Fatal("existing term lost:", err)
		}
	}
}
