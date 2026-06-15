package olxvn

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "olxvn" {
		t.Errorf("Scheme = %q, want olxvn", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "olxvn" {
		t.Errorf("Identity.Binary = %q, want olxvn", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct{ in, typ, id string }{
		{"xe-may-yamaha-abc123", "listing", "xe-may-yamaha-abc123"},
		{"item/xe-may-yamaha-abc123", "listing", "xe-may-yamaha-abc123"},
		{"https://" + Host + "/item/dien-thoai-iphone-def456/", "listing", "dien-thoai-iphone-def456"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestLocate(t *testing.T) {
	got, err := Domain{}.Locate("listing", "xe-may-yamaha-abc123")
	want := baseURL + "/item/xe-may-yamaha-abc123/"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "x")
	if err == nil {
		t.Error("expected error for unknown type, got nil")
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	l := &Listing{
		Slug:  "xe-may-yamaha-abc123",
		URL:   baseURL + "/item/xe-may-yamaha-abc123/",
		Title: "Xe máy Yamaha Exciter 2020",
		Price: 25000000,
	}
	u, err := h.Mint(l)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	want := "olxvn://listing/xe-may-yamaha-abc123"
	if u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("olxvn", "laptop-dell-ghi789")
	if err != nil || got.String() != "olxvn://listing/laptop-dell-ghi789" {
		t.Errorf("ResolveOn = (%q, %v), want olxvn://listing/laptop-dell-ghi789", got.String(), err)
	}
}
