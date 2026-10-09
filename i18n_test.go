package main

import "testing"

func TestEnTranslate(t *testing.T) {
	for in, want := range map[string]string{
		"연결되어 있지 않습니다":                     "Not connected",
		"  다운로드 ":                          "  Download ",
		"점프 호스트 b1에서 web:22에 접속하지 못했습니다: EOF": "The jump host b1 could not reach web:22: EOF",
		"3개 항목을 삭제했습니다":                    "Deleted 3 item(s)",
		// order of the parts changes; a part is translated too
		"EUC-KR에 없는 글자가 2개 있습니다. ?로 바꿔서 저장할까요?": "2 characters don't exist in EUC-KR. Save them as ?",
		"\"a.txt\"을(를) 서버에서 삭제할까요? 되돌릴 수 없습니다.": "Delete \"a.txt\" on the server? This can't be undone.",
		"3개 항목을(를) 서버에서 삭제할까요? 되돌릴 수 없습니다.":  "Delete 3 items on the server? This can't be undone.",
		// line by line when the whole text has no entry
		"x 호스트를 처음 접속합니다.\n\n키 지문: SHA256:abc\n\n이 호스트를 신뢰하고 계속하시겠습니까?": "First connection to the host x.\n\nKey fingerprint: SHA256:abc\n\nTrust this host and continue?",
		"사용자 파일 이름.txt": "사용자 파일 이름.txt", // unknown: unchanged
		"plain English":    "plain English",
	} {
		if got := enTranslate(in, 0); got != want {
			t.Errorf("%q:\n got %q\nwant %q", in, got, want)
		}
	}
}
