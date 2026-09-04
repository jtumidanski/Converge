package diff

import "testing"

func TestParseRaw(t *testing.T) {
	in := []byte(":000000 100644 0000000000000000000000000000000000000000 1111111111111111111111111111111111111111 A\x00new.txt\x00" +
		":100644 100644 2222222222222222222222222222222222222222 3333333333333333333333333333333333333333 M\x00mod.txt\x00" +
		":100644 000000 4444444444444444444444444444444444444444 0000000000000000000000000000000000000000 D\x00gone.txt\x00" +
		":100644 100644 5555555555555555555555555555555555555555 6666666666666666666666666666666666666666 R095\x00old/name.go\x00new/name.go\x00")
	got, err := parseRaw(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []rawEntry{{"new.txt", "", StatusAdded}, {"mod.txt", "", StatusModified}, {"gone.txt", "", StatusDeleted}, {"new/name.go", "old/name.go", StatusRenamed}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, got[i], want[i])
		}
	}
	if _, err := parseRaw([]byte(":100644 100644 a b M\x00")); err == nil {
		t.Error("truncated record accepted")
	}
}

func TestParseNumstat(t *testing.T) {
	in := []byte("3\t1\tmod.txt\x00-\t-\timg.png\x0010\t0\t\x00old/name.go\x00new/name.go\x00")
	got, err := parseNumstat(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != (numstatEntry{"mod.txt", "", 3, 1, false}) || got[1] != (numstatEntry{"img.png", "", 0, 0, true}) || got[2] != (numstatEntry{"new/name.go", "old/name.go", 10, 0, false}) {
		t.Errorf("got %+v", got)
	}
}
