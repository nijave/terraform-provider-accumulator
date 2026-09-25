// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import "time"

// FilterExpired drops values whose stored stamp is not strictly after now
// (spec X1, the refresh rule). Values without a stamp are never dropped.
// Read calls it with the refresh clock; the Update fallback with the apply
// clock. Both returns are non-nil, and the returned stamp map only holds
// survivors' stamps.
func FilterExpired(values []string, stamps map[string]time.Time, now time.Time) ([]string, map[string]time.Time) {
	survivors := make([]string, 0, len(values))
	kept := make(map[string]time.Time, len(stamps))
	for _, v := range values {
		stamp, ok := stamps[v]
		if ok && !stamp.After(now) {
			continue
		}
		survivors = append(survivors, v)
		if ok {
			kept[v] = stamp
		}
	}
	return survivors, kept
}

// ClassifyStamps partitions the stampable survivors — values in survivors
// that are not in planInputs — into keep and fresh. keep holds prior
// stamps that survive unchanged: plannedExpiresAfter is set, equals
// priorExpiresAfter, and the value has a prior stamp (S2). fresh holds
// every other stampable value when plannedExpiresAfter is set: no prior
// stamp (S1, S5), no prior expiresAfter (S5), or a changed
// expiresAfter (S3, S4). Values in planInputs, and everything under a
// null plannedExpiresAfter, belong to neither: their expires_at is null.
// It reads no clock; ModifyPlan uses it to decide which detailed_outputs
// entries are known and which stay unknown until the apply stamps them.
func ClassifyStamps(survivors, planInputs []string,
	prior map[string]time.Time,
	priorExpiresAfter, plannedExpiresAfter *int64,
) (keep map[string]time.Time, fresh map[string]struct{}) {
	keep = make(map[string]time.Time)
	fresh = make(map[string]struct{})
	if plannedExpiresAfter == nil {
		return keep, fresh
	}
	supplied := make(map[string]struct{}, len(planInputs))
	for _, v := range planInputs {
		supplied[v] = struct{}{}
	}
	for _, v := range survivors {
		if _, ok := supplied[v]; ok {
			continue
		}
		stamp, had := prior[v]
		unchanged := priorExpiresAfter != nil && *priorExpiresAfter == *plannedExpiresAfter
		if had && unchanged {
			keep[v] = stamp
			continue
		}
		fresh[v] = struct{}{}
	}
	return keep, fresh
}

// FreshStamp computes one fresh value's apply-time stamp: the candidate
// now + plannedExpiresAfter, kept as min(old, candidate) on a decrease and
// max(old, candidate) on an increase when a prior stamp exists (S1, S3,
// S4, S5).
func FreshStamp(prior map[string]time.Time,
	priorExpiresAfter, plannedExpiresAfter *int64,
	now time.Time, value string) time.Time {
	candidate := now.Add(time.Duration(*plannedExpiresAfter) * time.Second)
	stamp, had := prior[value]
	switch {
	case !had || priorExpiresAfter == nil:
		return candidate
	case *priorExpiresAfter > *plannedExpiresAfter:
		if candidate.Before(stamp) {
			return candidate
		}
	case *priorExpiresAfter < *plannedExpiresAfter:
		if candidate.After(stamp) {
			return candidate
		}
	}
	return stamp
}

// ApplyExpiration filters an accumulated result for expiry and returns the
// survivors with their stamps. It is the fallback-path composition (spec
// section 7): classify, stamp fresh, then filter expired, all with the one
// clock the fallback owns. The plan-unknown path gets the same decisions as
// Read plus Update in one call; the known paths use FilterExpired,
// ClassifyStamps, and FreshStamp instead.
//
// Per value of result:
//
//   - plannedExpiresAfter nil: nothing expires and nothing is stamped.
//   - a value in planInputs survives with no stamp; it cannot expire.
//   - otherwise the stamp is candidate = now + plannedExpiresAfter when
//     there is no prior stamp or no prior expiresAfter; min(prior,
//     candidate) on a decrease; max(prior, candidate) on an increase; the
//     prior stamp unchanged when expiresAfter did not change.
//   - after stamping, a value whose stamp is not strictly after now is
//     removed. An increase can therefore rescue an expired-but-unrealized
//     value, and expiresAfter 0 removes a value in the same call that
//     stamps it.
//
// now must already be truncated to whole seconds (clockNow in
// internal/provider is the choke point) so stamps round-trip RFC 3339 state
// strings without losing a fraction of a second.
//
// Both return values are non-nil. Stamps are only present for survivors not
// in planInputs; absence means expires_at is null.
func ApplyExpiration(result, planInputs []string,
	prior map[string]time.Time,
	priorExpiresAfter, plannedExpiresAfter *int64,
	now time.Time) ([]string, map[string]time.Time) {

	survivors := make([]string, 0, len(result))
	stamps := make(map[string]time.Time, len(result))
	if plannedExpiresAfter == nil {
		return append(survivors, result...), stamps
	}

	supplied := make(map[string]struct{}, len(planInputs))
	for _, v := range planInputs {
		supplied[v] = struct{}{}
	}

	for _, v := range result {
		if _, ok := supplied[v]; ok {
			survivors = append(survivors, v)
			continue
		}
		stamp := FreshStamp(prior, priorExpiresAfter, plannedExpiresAfter, now, v)
		if !stamp.After(now) {
			continue
		}
		survivors = append(survivors, v)
		stamps[v] = stamp
	}
	return survivors, stamps
}
