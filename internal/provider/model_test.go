// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestListFromStringsIsNeverNull(t *testing.T) {
	t.Parallel()

	for label, values := range map[string][]string{
		"nil":    nil,
		"empty":  {},
		"values": {"a", "b"},
	} {
		values := values
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, diags := listFromStrings(context.Background(), values)
			if diags.HasError() {
				t.Fatalf("listFromStrings: %v", diags.Errors())
			}
			if got.IsNull() {
				t.Fatal("listFromStrings produced a null list; state must always hold a concrete value")
			}
			if got.IsUnknown() {
				t.Fatal("listFromStrings produced an unknown list")
			}
		})
	}
}

func TestSetFromStringsIsNeverNull(t *testing.T) {
	t.Parallel()

	for label, values := range map[string][]string{
		"nil":   nil,
		"empty": {},
	} {
		values := values
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, diags := setFromStrings(context.Background(), values)
			if diags.HasError() {
				t.Fatalf("setFromStrings: %v", diags.Errors())
			}
			if got.IsNull() {
				t.Fatal("setFromStrings produced a null set; state must always hold a concrete value")
			}
		})
	}
}

// TestSetFromStringsRequiresUniqueValues pins the framework behavior the
// resources depend on: a duplicate is a diagnostic, not a silent collapse.
// Every caller passes accumulate.Merge's output, which is unique by
// construction, so the diagnostic is unreachable in practice and is a bug
// signal if it ever appears.
func TestSetFromStringsRequiresUniqueValues(t *testing.T) {
	t.Parallel()
	got, diags := setFromStrings(context.Background(), []string{"a", "b", "a"})
	if !diags.HasError() {
		t.Fatalf("setFromStrings accepted duplicates and produced %v", got)
	}
}

func TestStringsFromListRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	list, diags := listFromStrings(ctx, []string{"a", "b"})
	if diags.HasError() {
		t.Fatalf("listFromStrings: %v", diags.Errors())
	}
	got, diags := stringsFromList(ctx, list)
	if diags.HasError() {
		t.Fatalf("stringsFromList: %v", diags.Errors())
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("round trip produced %v, want [a b]", got)
	}
}

func TestStringsFromListNullIsEmpty(t *testing.T) {
	t.Parallel()
	got, diags := stringsFromList(context.Background(), types.ListNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("stringsFromList: %v", diags.Errors())
	}
	if got != nil {
		t.Fatalf("stringsFromList(null) = %v, want nil", got)
	}
}

func TestStringsFromSetRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	set, diags := setFromStrings(ctx, []string{"a", "b"})
	if diags.HasError() {
		t.Fatalf("setFromStrings: %v", diags.Errors())
	}
	got, diags := stringsFromSet(ctx, set)
	if diags.HasError() {
		t.Fatalf("stringsFromSet: %v", diags.Errors())
	}
	if len(got) != 2 {
		t.Fatalf("round trip produced %v, want two elements", got)
	}
}

func TestStringsFromSetNullIsEmpty(t *testing.T) {
	t.Parallel()
	got, diags := stringsFromSet(context.Background(), types.SetNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("stringsFromSet: %v", diags.Errors())
	}
	if got != nil {
		t.Fatalf("stringsFromSet(null) = %v, want nil", got)
	}
}
