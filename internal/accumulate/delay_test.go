// SPDX-License-Identifier: MPL-2.0

package accumulate

import (
	"reflect"
	"testing"
	"time"
)

// TestFilterPending pins the reveal rule V1 as Read applies it: a stamp not
// strictly after now is due, so the value is visible and its stamp dropped;
// a future stamp keeps the value pending.
func TestFilterPending(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values      []string
		stamps      map[string]time.Time
		now         time.Time
		wantVisible []string
		wantPending map[string]time.Time
	}{
		"stamp equal to now is revealed (V1 inclusive)": {
			values:      []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase},
			now:         stampBase,
			wantVisible: []string{"a"},
			wantPending: map[string]time.Time{},
		},
		"stamp strictly after now stays pending": {
			values:      []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Second)},
			now:         stampBase,
			wantVisible: []string{},
			wantPending: map[string]time.Time{"a": stampBase.Add(time.Second)},
		},
		"unstamped values are visible": {
			values:      []string{"a", "b"},
			stamps:      map[string]time.Time{"b": stampBase.Add(time.Hour)},
			now:         stampBase,
			wantVisible: []string{"a"},
			wantPending: map[string]time.Time{"b": stampBase.Add(time.Hour)},
		},
		"nil stamps reveals everything": {
			values:      []string{"a", "b"},
			stamps:      nil,
			now:         stampBase,
			wantVisible: []string{"a", "b"},
			wantPending: map[string]time.Time{},
		},
		"empty input yields empty output": {
			values:      []string{},
			stamps:      map[string]time.Time{},
			now:         stampBase,
			wantVisible: []string{},
			wantPending: map[string]time.Time{},
		},
		"order is preserved and duplicates share one decision": {
			values:      []string{"a", "b", "a", "c"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Hour), "c": stampBase.Add(-time.Second)},
			now:         stampBase,
			wantVisible: []string{"b", "c"},
			wantPending: map[string]time.Time{"a": stampBase.Add(time.Hour)},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			visible, pending := FilterPending(tc.values, tc.stamps, tc.now)
			if !reflect.DeepEqual(visible, tc.wantVisible) {
				t.Fatalf("visible = %v, want %v", visible, tc.wantVisible)
			}
			if visible == nil {
				t.Fatal("visible is nil; it must always be non-nil")
			}
			if !reflect.DeepEqual(pending, tc.wantPending) {
				t.Fatalf("pending = %v, want %v", pending, tc.wantPending)
			}
			if pending == nil {
				t.Fatal("pending is nil; it must always be non-nil")
			}
		})
	}
}

// TestClassifyDelays pins the clock-free split ModifyPlan uses (spec D1 to
// D5, T2): entering values are fresh, pending values under an unchanged
// delay are keepers, pending values under a changed delay are fresh, and
// visible values or anything under a null planned delay are in neither.
func TestClassifyDelays(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		next      []string
		prior     []string
		stamps    map[string]time.Time
		priorD    *int64
		plannedD  *int64
		wantKeep  map[string]time.Time
		wantFresh map[string]struct{}
	}{
		"entering value is fresh (D1)": {
			next:      []string{"a", "b"},
			prior:     []string{"a"},
			stamps:    nil,
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"b": {}},
		},
		"pending value under an unchanged delay is kept (D2)": {
			next:      []string{"a"},
			prior:     []string{"a"},
			stamps:    map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:    seconds(300),
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{"a": stampBase.Add(time.Hour)},
			wantFresh: map[string]struct{}{},
		},
		"pending value under a decreased delay is fresh (D3)": {
			next:      []string{"a"},
			prior:     []string{"a"},
			stamps:    map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:    seconds(3600),
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"a": {}},
		},
		"pending value under an increased delay is fresh (D4)": {
			next:      []string{"a"},
			prior:     []string{"a"},
			stamps:    map[string]time.Time{"a": stampBase.Add(time.Minute)},
			priorD:    seconds(60),
			plannedD:  seconds(3600),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"a": {}},
		},
		"visible prior value is in neither map (D5)": {
			next:      []string{"a", "b"},
			prior:     []string{"a", "b"},
			stamps:    map[string]time.Time{},
			priorD:    nil,
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{},
		},
		"null planned delay reveals everything (T2)": {
			next:      []string{"a", "b"},
			prior:     []string{"a"},
			stamps:    map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:    seconds(300),
			plannedD:  nil,
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{},
		},
		"nil prior means everything enters (create and reseed, T4)": {
			next:      []string{"a", "b"},
			prior:     nil,
			stamps:    nil,
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"a": {}, "b": {}},
		},
		"duplicates in next classify once (list shape)": {
			next:      []string{"a", "a"},
			prior:     nil,
			stamps:    nil,
			plannedD:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"a": {}},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			keep, fresh := ClassifyDelays(tc.next, tc.prior, tc.stamps, tc.priorD, tc.plannedD)
			if !reflect.DeepEqual(keep, tc.wantKeep) {
				t.Fatalf("keep = %v, want %v", keep, tc.wantKeep)
			}
			if !reflect.DeepEqual(fresh, tc.wantFresh) {
				t.Fatalf("fresh = %v, want %v", fresh, tc.wantFresh)
			}
			if keep == nil || fresh == nil {
				t.Fatal("keep and fresh must always be non-nil")
			}
		})
	}
}

// TestVisible pins the invariant as a function: next minus every value in
// keep or fresh, order preserved, never nil.
func TestVisible(t *testing.T) {
	t.Parallel()

	got := Visible([]string{"a", "b", "c", "b"},
		map[string]time.Time{"b": stampBase},
		map[string]struct{}{"c": {}})
	if want := []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Visible = %v, want %v", got, want)
	}
	if got := Visible(nil, nil, nil); got == nil || len(got) != 0 {
		t.Fatalf("Visible(nil) = %#v, want empty non-nil", got)
	}
	if got := Visible([]string{"a", "b"}, nil, nil); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Visible with nothing hidden = %v, want [a b]", got)
	}
}

// TestApplyDelay pins the fallback composition: classify, stamp fresh with
// the apply clock, hide every stamped value. It never reveals; that is
// refresh's job (spec V1), so a zero delay leaves the value hidden here.
func TestApplyDelay(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		next        []string
		prior       []string
		stamps      map[string]time.Time
		priorD      *int64
		plannedD    *int64
		wantVisible []string
		wantStamps  map[string]time.Time
	}{
		"entering value is stamped now+D and hidden (D1)": {
			next:        []string{"a", "b"},
			prior:       []string{"a"},
			stamps:      nil,
			plannedD:    seconds(300),
			wantVisible: []string{"a"},
			wantStamps:  map[string]time.Time{"b": stampBase.Add(5 * time.Minute)},
		},
		"D removed reveals everything and stamps nothing (T2)": {
			next:        []string{"a", "b"},
			prior:       []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:      seconds(300),
			plannedD:    nil,
			wantVisible: []string{"a", "b"},
			wantStamps:  map[string]time.Time{},
		},
		"D of zero stamps now and still hides (V1 is refresh's job)": {
			next:        []string{"a"},
			prior:       nil,
			stamps:      nil,
			plannedD:    seconds(0),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{"a": stampBase},
		},
		"unchanged D keeps the prior stamp (D2)": {
			next:        []string{"a"},
			prior:       []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:      seconds(3600),
			plannedD:    seconds(3600),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{"a": stampBase.Add(time.Hour)},
		},
		"decrease keeps min(old, candidate) (D3)": {
			next:        []string{"a"},
			prior:       []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Hour)},
			priorD:      seconds(3600),
			plannedD:    seconds(300),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{"a": stampBase.Add(5 * time.Minute)},
		},
		"increase keeps max(old, candidate) (D4)": {
			next:        []string{"a"},
			prior:       []string{"a"},
			stamps:      map[string]time.Time{"a": stampBase.Add(time.Minute)},
			priorD:      seconds(60),
			plannedD:    seconds(3600),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{"a": stampBase.Add(time.Hour)},
		},
		"reseed ignores prior stamps: nil prior restamps a full window (T4)": {
			// The caller passes nil prior and nil stamps on reseed; a value
			// that was pending before the reset gets now+D, not its old stamp.
			next:        []string{"a"},
			prior:       nil,
			stamps:      nil,
			priorD:      seconds(300),
			plannedD:    seconds(300),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{"a": stampBase.Add(5 * time.Minute)},
		},
		"visible prior value stays visible when D is set later (D5)": {
			next:        []string{"a", "b"},
			prior:       []string{"a"},
			stamps:      map[string]time.Time{},
			priorD:      nil,
			plannedD:    seconds(300),
			wantVisible: []string{"a"},
			wantStamps:  map[string]time.Time{"b": stampBase.Add(5 * time.Minute)},
		},
		"empty next yields empty results": {
			next:        []string{},
			prior:       nil,
			plannedD:    seconds(300),
			wantVisible: []string{},
			wantStamps:  map[string]time.Time{},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			visible, stamps := ApplyDelay(tc.next, tc.prior, tc.stamps, tc.priorD, tc.plannedD, stampBase)
			if !reflect.DeepEqual(visible, tc.wantVisible) {
				t.Fatalf("visible = %v, want %v", visible, tc.wantVisible)
			}
			if !reflect.DeepEqual(stamps, tc.wantStamps) {
				t.Fatalf("stamps = %v, want %v", stamps, tc.wantStamps)
			}
			if visible == nil || stamps == nil {
				t.Fatal("visible and stamps must always be non-nil")
			}
		})
	}
}
