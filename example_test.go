package guangyapan_test

import (
	"fmt"
	"github.com/shensongpeng/guangyapan-go"
)

func ExamplePKCEChallenge() {
	fmt.Println(guangyapan.PKCEChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
	// Output: E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM
}

func ExampleParseOAuthCallback() {
	code, err := guangyapan.ParseOAuthCallback("https://app.example/callback?code=abc&state=expected", "expected")
	fmt.Println(code, err)
	// Output: abc <nil>
}
