package holes

import "testing"

func TestPlaceholderID(t *testing.T) {
	id := PlaceholderID(34, "abc@nyuu")
	if !IsPlaceholderID(id) || !IsPlaceholderID("<"+id+">") {
		t.Fatalf("PlaceholderID %q not recognised", id)
	}
	if PlaceholderID(6, "abc@nyuu") == id {
		t.Fatal("different segment numbers must give different ids")
	}
	if PlaceholderID(34, "other@nyuu") == id {
		t.Fatal("different salts must give different ids")
	}
	for _, real := range []string{"", "abc@nyuu", "<S4oV29iryP4atfw4YCW9_1o17@JBinUp.local>", "altmount-gapless"} {
		if IsPlaceholderID(real) {
			t.Fatalf("%q wrongly recognised as placeholder", real)
		}
	}
}
