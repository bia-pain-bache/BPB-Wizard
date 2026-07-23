package internal

import (
	"fmt"
	mrand "math/rand"
	"strings"
)

func randInt(min, max int) int {
	return mrand.Intn(max-min+1) + min
}

func randString(charset string, minLen, maxLen int) string {
	length := minLen + mrand.Intn(maxLen-minLen+1)
	bytes := make([]byte, length)
	for i := range bytes {
		bytes[i] = charset[mrand.Intn(len(charset))]
	}
	
	return string(bytes)
}

func randSubdomain() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789-"
	for {
		subdomain := randString(charset, 16, 32)
		if !strings.HasPrefix(subdomain, "-") && !strings.HasSuffix(subdomain, "-") {
			return subdomain, nil
		}
	}
}

func randCode() string {
	const minVars, maxVars = 50, 500
	const minFuncs, maxFuncs = 50, 500
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	varCount := randInt(minVars, maxVars)
	funcCount := randInt(minFuncs, maxFuncs)

	var varsBuilder strings.Builder
	for i := range varCount {
		varName := fmt.Sprintf("__var_%s_%d", randString(charset, 8,16), i)
		value := randInt(0, 99999)
		varsBuilder.WriteString(fmt.Sprintf("let %s = %d;\n", varName, value))
	}

	var funcsBuilder strings.Builder
	for i := range funcCount {
		funcName := fmt.Sprintf("__func_%s_%d", randString(charset, 8,16), i)
		value := randInt(0, 999)
		fmt.Fprintf(&funcsBuilder, "function %s() { return %d; }\n", funcName, value)
	}

	return varsBuilder.String() + funcsBuilder.String()
}
