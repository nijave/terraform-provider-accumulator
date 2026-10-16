// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import "time"

// FilterPending partitions values by their stored visible_at stamps (spec
// V1, the reveal rule). A value with no stamp, or a stamp not strictly
// after now, is visible; the rest are pending. Order is preserved. pending
// holds only the stamps that are still in the future, so the caller writes
// every revealed entry back as null. Read calls it with the refresh clock.
// Both returns are non-nil.
func FilterPending(values []string, stamps map[string]time.Time, now time.Time) (visible []string, pending map[string]time.Time) {
	visible = make([]string, 0, len(values))
	pending = make(map[string]time.Time, len(stamps))
	for _, v := range values {
		stamp, ok := stamps[v]
		if ok && stamp.After(now) {
			pending[v] = stamp
			continue
		}
		visible = append(visible, v)
	}
	return visible, pending
}

// ClassifyDelays decides each value of next without a clock (spec D1 to
// D5, T2). keep holds prior pending stamps that survive unchanged: the
// value was in prior, has a stamp, and plannedDelayedFor equals
// priorDelayedFor (D2). fresh holds every value the apply must stamp: a
// value not in prior (D1, entering), or a pending value whose delay changed
// (D3, D4). A value in prior with no stamp is visible and appears in
// neither map (D5). When plannedDelayedFor is nil nothing is pending (T2).
// Callers pass nil prior and nil priorStamps on a reseed so every value
// enters with a full window (T4). Both returns are non-nil.
func ClassifyDelays(next, prior []string,
	priorStamps map[string]time.Time,
	priorDelayedFor, plannedDelayedFor *int64,
) (keep map[string]time.Time, fresh map[string]struct{}) {
	keep = make(map[string]time.Time)
	fresh = make(map[string]struct{})
	if plannedDelayedFor == nil {
		return keep, fresh
	}
	present := make(map[string]struct{}, len(prior))
	for _, v := range prior {
		present[v] = struct{}{}
	}
	unchanged := priorDelayedFor != nil && *priorDelayedFor == *plannedDelayedFor
	for _, v := range next {
		if _, wasPresent := present[v]; !wasPresent {
			fresh[v] = struct{}{}
			continue
		}
		stamp, pending := priorStamps[v]
		switch {
		case !pending:
			// Already visible: setting or changing delayed_for never hides it.
		case unchanged:
			keep[v] = stamp
		default:
			fresh[v] = struct{}{}
		}
	}
	return keep, fresh
}

// Visible returns next minus every value present in keep or fresh,
// preserving order. It is the invariant of spec section 3 as a function:
// outputs is accumulated minus the pending values. The result is non-nil.
func Visible(next []string, keep map[string]time.Time, fresh map[string]struct{}) []string {
	visible := make([]string, 0, len(next))
	for _, v := range next {
		if _, ok := keep[v]; ok {
			continue
		}
		if _, ok := fresh[v]; ok {
			continue
		}
		visible = append(visible, v)
	}
	return visible
}

// ApplyDelay is the fallback-path composition: classify, stamp fresh
// entries with the apply clock, and hide every stamped value. It never
// reveals: a stamp of exactly now still hides the value until the next
// refresh (spec V1), which is also what makes delayed_for = 0 mean "visible
// after the next refresh". now must be truncated to whole seconds
// (clockNow in internal/provider). Both returns are non-nil; stamps holds
// every pending value.
func ApplyDelay(next, prior []string,
	priorStamps map[string]time.Time,
	priorDelayedFor, plannedDelayedFor *int64,
	now time.Time) (visible []string, stamps map[string]time.Time) {

	keep, fresh := ClassifyDelays(next, prior, priorStamps, priorDelayedFor, plannedDelayedFor)
	stamps = make(map[string]time.Time, len(keep)+len(fresh))
	for v, stamp := range keep {
		stamps[v] = stamp
	}
	for v := range fresh {
		stamps[v] = FreshStamp(priorStamps, priorDelayedFor, plannedDelayedFor, now, v)
	}
	return Visible(next, keep, fresh), stamps
}
