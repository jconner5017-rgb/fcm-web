package lister

import "testing"

func TestPlainText(t *testing.T) {
	got := plainText(`<style>p{}</style><h2>Goku &amp; Friends</h2><p>Near <b>Mint</b></p><img src="x"><p style="a">Provided by FireCardManager.com</p>`)
	if got != "Goku & Friends\nNear Mint" {
		t.Fatalf("%q", got)
	}
	if bothStatus("listed", "skipped") != "listed" || bothStatus("updated", "failed") != "failed" || bothStatus("skipped", "skipped") != "skipped" {
		t.Fatal("bothStatus")
	}
}
