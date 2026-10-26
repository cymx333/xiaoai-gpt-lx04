// Copyright 2026 cymx333 and contributors.
// SPDX-License-Identifier: Apache-2.0
package main

import "testing"

func TestPartialAndFinal(t *testing.T) {
	for _, tc := range []struct{ line, kind string }{
		{`instruction={"header":{"namespace":"SpeechRecognizer","name":"RecognizeResult"},"payload":{"is_final":false,"results":[{"text":"问题"}]}}`, "listening"},
		{`instruction={"header":{"namespace":"SpeechRecognizer","name":"RecognizeResult"},"payload":{"is_final":true,"results":[{"text":"问题"}]}}`, "query"},
		{`instruction={"header":{"namespace":"SpeechSynthesizer","name":"Speak"},"payload":{"text":"答案"}}`, "speak"},
		{`instruction={"header":{"namespace":"SpeechRecognizer","name":"RecognizeResult"},"payload":{"is_final":false,"results":[]}}`, ""},
	} {
		if e := parse(tc.line); e.Kind != tc.kind {
			t.Fatalf("got %q want %q", e.Kind, tc.kind)
		}
	}
}
