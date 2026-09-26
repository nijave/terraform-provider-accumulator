// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"reflect"
	"testing"
	"time"
)

// seconds is the test shorthand for the *int64 expires_after parameters.
func seconds(n int64) *int64 { return &n }

// stampBase is the fixed clock every case computes against. now must be
// truncated to whole seconds, matching clockNow in internal/provider, so
// stamps formatted RFC 3339 round-trip exactly.
var stampBase = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

// TestApplyExpiration pins the whole stamping and removal decision. Every
// rule letter cites the spec (2026-09-23-accumulator-expiration-design.md,
// section 4).
func TestApplyExpiration(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		result    []string
		inputs    []string
		prior     map[string]time.Time
		priorE    *int64
		plannedE  *int64
		want      []string
		wantStamp map[string]time.Time
	}{
		"leaver with no prior stamp is stamped now+E (S1)": {
			result:   []string{"a", "b"},
			inputs:   []string{"a"},
			prior:    nil,
			plannedE: seconds(300),
			want:     []string{"a", "b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(5 * time.Minute),
			},
		},
		"in-inputs value is never stamped and never removed (R2)": {
			result:    []string{"a"},
			inputs:    []string{"a"},
			prior:     map[string]time.Time{"a": stampBase.Add(-1 * time.Hour)},
			plannedE:  seconds(1),
			want:      []string{"a"},
			wantStamp: map[string]time.Time{},
		},
		"removal boundary is inclusive (X1)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase},
			priorE:   seconds(300),
			plannedE: seconds(300),
			want:     []string{},
		},
		"future stamp is kept unchanged (S2, X1 negative)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(1 * time.Second)},
			priorE:   seconds(300),
			plannedE: seconds(300),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(1 * time.Second),
			},
		},
		"no prior stamp with E just set gets the candidate (S5)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    nil,
			priorE:   nil,
			plannedE: seconds(300),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(5 * time.Minute),
			},
		},
		"stamped value with E set later keeps no clock until E exists (S5 via null priorE)": {
			// History that accumulated while E was null: prior stamp absent,
			// priorE nil, so the first E-setting plan stamps the candidate.
			result:   []string{"a", "b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{},
			priorE:   nil,
			plannedE: seconds(60),
			want:     []string{"a", "b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(1 * time.Minute),
			},
		},
		"decrease keeps min(old, candidate) when candidate is earlier (S3)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(10 * time.Minute)},
			priorE:   seconds(600),
			plannedE: seconds(300),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(5 * time.Minute),
			},
		},
		"decrease keeps min(old, candidate) when old is earlier (S3)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(1 * time.Minute)},
			priorE:   seconds(600),
			plannedE: seconds(300),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(1 * time.Minute),
			},
		},
		"increase keeps max(old, candidate) when candidate is later (S4)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(1 * time.Minute)},
			priorE:   seconds(60),
			plannedE: seconds(600),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(10 * time.Minute),
			},
		},
		"increase keeps max(old, candidate) when old is later (S4)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(10 * time.Minute)},
			priorE:   seconds(60),
			plannedE: seconds(300),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(10 * time.Minute),
			},
		},
		"increase rescues an expired-unrealized value (S4 plus X1)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(-1 * time.Minute)},
			priorE:   seconds(60),
			plannedE: seconds(600),
			want:     []string{"b"},
			wantStamp: map[string]time.Time{
				"b": stampBase.Add(10 * time.Minute),
			},
		},
		"decrease never rescues an expired-unrealized value (S3 plus X1)": {
			result:   []string{"b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(-1 * time.Minute)},
			priorE:   seconds(600),
			plannedE: seconds(300),
			want:     []string{},
		},
		"E removed expires nothing and stamps nothing (T2)": {
			result:   []string{"a", "b"},
			inputs:   []string{"a"},
			prior:    map[string]time.Time{"b": stampBase.Add(-1 * time.Minute)},
			priorE:   seconds(60),
			plannedE: nil,
			want:     []string{"a", "b"},
		},
		"E of zero removes a leaver at the same call (X1)": {
			result:   []string{"a", "b"},
			inputs:   []string{"a"},
			prior:    nil,
			plannedE: seconds(0),
			want:     []string{"a"},
		},
		"duplicate values expire together and order is preserved (list shape)": {
			result:   []string{"a", "b", "b", "c"},
			inputs:   []string{"a", "c"},
			prior:    map[string]time.Time{"b": stampBase.Add(-1 * time.Second)},
			priorE:   seconds(300),
			plannedE: seconds(300),
			want:     []string{"a", "c"},
		},
		"create shape: everything in inputs yields no stamps (T4)": {
			result:    []string{"a", "b"},
			inputs:    []string{"a", "b"},
			prior:     map[string]time.Time{"a": stampBase.Add(-1 * time.Hour)},
			plannedE:  seconds(300),
			want:      []string{"a", "b"},
			wantStamp: map[string]time.Time{},
		},
		"empty result yields empty survivors and empty stamps": {
			result:    []string{},
			inputs:    nil,
			plannedE:  seconds(300),
			want:      []string{},
			wantStamp: map[string]time.Time{},
		},
		"nil result with E set yields empty survivors": {
			result:   nil,
			inputs:   nil,
			plannedE: seconds(300),
			want:     []string{},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, gotStamps := ApplyExpiration(tc.result, tc.inputs, tc.prior, tc.priorE, tc.plannedE, stampBase)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("survivors = %v, want %v", got, tc.want)
			}
			if got == nil {
				t.Fatal("survivors is nil; it must always be non-nil")
			}
			wantStamps := tc.wantStamp
			if wantStamps == nil {
				wantStamps = map[string]time.Time{}
			}
			if !reflect.DeepEqual(gotStamps, wantStamps) {
				t.Fatalf("stamps = %v, want %v", gotStamps, wantStamps)
			}
			if gotStamps == nil {
				t.Fatal("stamps is nil; it must always be non-nil")
			}
		})
	}
}

// TestApplyExpirationIsDeterministic pins that the clock is the only input
// that varies: identical arguments produce identical results (spec 10.1).
func TestApplyExpirationIsDeterministic(t *testing.T) {
	t.Parallel()
	args := []any{
		[]string{"a", "b"}, []string{"a"},
		map[string]time.Time{"b": stampBase.Add(time.Minute)},
		seconds(60), seconds(300), stampBase,
	}
	first, firstStamps := ApplyExpiration(
		args[0].([]string), args[1].([]string), args[2].(map[string]time.Time),
		args[3].(*int64), args[4].(*int64), args[5].(time.Time))
	second, secondStamps := ApplyExpiration(
		args[0].([]string), args[1].([]string), args[2].(map[string]time.Time),
		args[3].(*int64), args[4].(*int64), args[5].(time.Time))
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstStamps, secondStamps) {
		t.Fatalf("ApplyExpiration is not deterministic: %v/%v vs %v/%v", first, firstStamps, second, secondStamps)
	}
}

// TestFilterExpired pins the refresh cull (spec X1): a value whose stored
// stamp is not strictly after now is dropped, and a value without a stamp is
// never dropped.
func TestFilterExpired(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values     []string
		stamps     map[string]time.Time
		now        time.Time
		want       []string
		wantStamps map[string]time.Time
	}{
		"drops a stamp equal to now (X1 inclusive)": {
			values:     []string{"a"},
			stamps:     map[string]time.Time{"a": stampBase},
			now:        stampBase,
			want:       []string{},
			wantStamps: map[string]time.Time{},
		},
		"keeps a stamp strictly after now": {
			values:     []string{"a"},
			stamps:     map[string]time.Time{"a": stampBase.Add(time.Second)},
			now:        stampBase,
			want:       []string{"a"},
			wantStamps: map[string]time.Time{"a": stampBase.Add(time.Second)},
		},
		"drops a stamp before now": {
			values:     []string{"a"},
			stamps:     map[string]time.Time{"a": stampBase.Add(-time.Second)},
			now:        stampBase,
			want:       []string{},
			wantStamps: map[string]time.Time{},
		},
		"never drops an unstamped value": {
			values:     []string{"a", "b"},
			stamps:     map[string]time.Time{"a": stampBase.Add(-time.Hour)},
			now:        stampBase,
			want:       []string{"b"},
			wantStamps: map[string]time.Time{},
		},
		"nil stamps keeps everything": {
			values:     []string{"a", "b"},
			stamps:     nil,
			now:        stampBase,
			want:       []string{"a", "b"},
			wantStamps: map[string]time.Time{},
		},
		"empty input yields empty output": {
			values:     []string{},
			stamps:     map[string]time.Time{},
			now:        stampBase,
			want:       []string{},
			wantStamps: map[string]time.Time{},
		},
		"order is preserved and only survivors' stamps are returned": {
			values:     []string{"a", "b", "c"},
			stamps:     map[string]time.Time{"b": stampBase.Add(-time.Second), "c": stampBase.Add(time.Hour)},
			now:        stampBase,
			want:       []string{"a", "c"},
			wantStamps: map[string]time.Time{"c": stampBase.Add(time.Hour)},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, gotStamps := FilterExpired(tc.values, tc.stamps, tc.now)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("survivors = %v, want %v", got, tc.want)
			}
			if got == nil {
				t.Fatal("survivors is nil; it must always be non-nil")
			}
			if !reflect.DeepEqual(gotStamps, tc.wantStamps) {
				t.Fatalf("stamps = %v, want %v", gotStamps, tc.wantStamps)
			}
			if gotStamps == nil {
				t.Fatal("stamps is nil; it must always be non-nil")
			}
		})
	}
}

// TestClassifyStamps pins the clock-free split ModifyPlan uses: keep holds
// prior stamps under an unchanged expiresAfter (S2), fresh holds everything
// the apply must stamp (S1, S3, S4, S5), and in-input or null-E values are in
// neither.
func TestClassifyStamps(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		survivors []string
		inputs    []string
		prior     map[string]time.Time
		priorE    *int64
		plannedE  *int64
		wantKeep  map[string]time.Time
		wantFresh map[string]struct{}
	}{
		"unchanged E keeps a prior stamp (S2)": {
			survivors: []string{"b"},
			inputs:    []string{"a"},
			prior:     map[string]time.Time{"b": stampBase.Add(time.Minute)},
			priorE:    seconds(300),
			plannedE:  seconds(300),
			wantKeep:  map[string]time.Time{"b": stampBase.Add(time.Minute)},
			wantFresh: map[string]struct{}{},
		},
		"no prior stamp is fresh (S1, S5)": {
			survivors: []string{"b"},
			inputs:    []string{"a"},
			prior:     map[string]time.Time{},
			priorE:    seconds(300),
			plannedE:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"b": {}},
		},
		"nil prior map is fresh (S5)": {
			survivors: []string{"b"},
			inputs:    []string{"a"},
			prior:     nil,
			priorE:    nil,
			plannedE:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"b": {}},
		},
		"null prior E is fresh even with a prior stamp (S5)": {
			survivors: []string{"b"},
			inputs:    []string{"a"},
			prior:     map[string]time.Time{"b": stampBase.Add(time.Minute)},
			priorE:    nil,
			plannedE:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"b": {}},
		},
		"decrease is fresh (S3)": {
			survivors: []string{"b"},
			inputs:    []string{"a"},
			prior:     map[string]time.Time{"b": stampBase.Add(10 * time.Minute)},
			priorE:    seconds(600),
			plannedE:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"b": {}},
		},
		"increase is fresh (S4)": {
			survivors: []string{"b"},
			inputs:    []string{"a"},
			prior:     map[string]time.Time{"b": stampBase.Add(time.Minute)},
			priorE:    seconds(60),
			plannedE:  seconds(600),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{"b": {}},
		},
		"in-inputs value is in neither": {
			survivors: []string{"a"},
			inputs:    []string{"a"},
			prior:     map[string]time.Time{"a": stampBase.Add(-time.Hour)},
			priorE:    seconds(300),
			plannedE:  seconds(300),
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{},
		},
		"null planned E puts every value in neither": {
			survivors: []string{"a", "b"},
			inputs:    []string{"a"},
			prior:     map[string]time.Time{"b": stampBase.Add(time.Minute)},
			priorE:    seconds(300),
			plannedE:  nil,
			wantKeep:  map[string]time.Time{},
			wantFresh: map[string]struct{}{},
		},
		"keeper and fresh split together": {
			survivors: []string{"a", "b", "c"},
			inputs:    []string{"a"},
			prior:     map[string]time.Time{"b": stampBase.Add(time.Minute)},
			priorE:    seconds(300),
			plannedE:  seconds(300),
			wantKeep:  map[string]time.Time{"b": stampBase.Add(time.Minute)},
			wantFresh: map[string]struct{}{"c": {}},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			keep, fresh := ClassifyStamps(tc.survivors, tc.inputs, tc.prior, tc.priorE, tc.plannedE)
			if !reflect.DeepEqual(keep, tc.wantKeep) {
				t.Fatalf("keep = %v, want %v", keep, tc.wantKeep)
			}
			if keep == nil {
				t.Fatal("keep is nil; it must always be non-nil")
			}
			if !reflect.DeepEqual(fresh, tc.wantFresh) {
				t.Fatalf("fresh = %v, want %v", fresh, tc.wantFresh)
			}
			if fresh == nil {
				t.Fatal("fresh is nil; it must always be non-nil")
			}
		})
	}
}

// TestFreshStamp pins each arm of the min/max switch, mirroring the
// ApplyExpiration table rows.
func TestFreshStamp(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		prior    map[string]time.Time
		priorE   *int64
		plannedE *int64
		want     time.Time
	}{
		"no prior stamp returns the candidate (S1, S5)": {
			prior:    nil,
			priorE:   nil,
			plannedE: seconds(300),
			want:     stampBase.Add(5 * time.Minute),
		},
		"no prior E returns the candidate even with a prior stamp (S5)": {
			prior:    map[string]time.Time{"b": stampBase.Add(time.Minute)},
			priorE:   nil,
			plannedE: seconds(300),
			want:     stampBase.Add(5 * time.Minute),
		},
		"decrease returns the earlier candidate (S3)": {
			prior:    map[string]time.Time{"b": stampBase.Add(10 * time.Minute)},
			priorE:   seconds(600),
			plannedE: seconds(300),
			want:     stampBase.Add(5 * time.Minute),
		},
		"decrease keeps the later prior stamp (S3)": {
			prior:    map[string]time.Time{"b": stampBase.Add(time.Minute)},
			priorE:   seconds(600),
			plannedE: seconds(300),
			want:     stampBase.Add(time.Minute),
		},
		"increase returns the later candidate (S4)": {
			prior:    map[string]time.Time{"b": stampBase.Add(time.Minute)},
			priorE:   seconds(60),
			plannedE: seconds(600),
			want:     stampBase.Add(10 * time.Minute),
		},
		"increase keeps the later prior stamp (S4)": {
			prior:    map[string]time.Time{"b": stampBase.Add(10 * time.Minute)},
			priorE:   seconds(60),
			plannedE: seconds(300),
			want:     stampBase.Add(10 * time.Minute),
		},
		"unchanged E returns the prior stamp (S2)": {
			prior:    map[string]time.Time{"b": stampBase.Add(time.Minute)},
			priorE:   seconds(300),
			plannedE: seconds(300),
			want:     stampBase.Add(time.Minute),
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := FreshStamp(tc.prior, tc.priorE, tc.plannedE, stampBase, "b")
			if !got.Equal(tc.want) {
				t.Fatalf("FreshStamp = %v, want %v", got, tc.want)
			}
		})
	}
}
