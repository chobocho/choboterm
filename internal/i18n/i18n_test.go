package i18n

import "testing"

func TestEnTranslate(t *testing.T) {
	for in, want := range map[string]string{
		"연결되어 있지 않습니다": "Not connected",
		"  다운로드 ":      "  Download ",
		"점프 호스트 b1에서 web:22에 접속하지 못했습니다: EOF": "The jump host b1 could not reach web:22: EOF",
		"3개 항목을 삭제했습니다":                       "Deleted 3 item(s)",
		// order of the parts changes; a part is translated too
		"EUC-KR에 없는 글자가 2개 있습니다. ?로 바꿔서 저장할까요?": "2 characters don't exist in EUC-KR. Save them as ?",
		"\"a.txt\"을(를) 서버에서 삭제할까요? 되돌릴 수 없습니다.": "Delete \"a.txt\" on the server? This can't be undone.",
		"3개 항목을(를) 서버에서 삭제할까요? 되돌릴 수 없습니다.":     "Delete 3 items on the server? This can't be undone.",
		// line by line when the whole text has no entry
		"x 호스트를 처음 접속합니다.\n\n키 지문: SHA256:abc\n\n이 호스트를 신뢰하고 계속하시겠습니까?": "First connection to the host x.\n\nKey fingerprint: SHA256:abc\n\nTrust this host and continue?",
		// the FTPS certificate question: each line, and the reason inside a line
		"x:21 서버의 인증서를 확인할 수 없습니다.\n\n이유: 믿을 수 있는 기관이 발급하지 않았습니다 (자체 서명 등)\n발급 대상: ftp\n유효 기간: 2026-01-01 ~ 2027-01-01\n지문: SHA256:abc\n\n이 인증서를 신뢰하고 계속하시겠습니까?": "The certificate of the server x:21 can't be verified.\n\nReason: Not issued by a trusted authority (self-signed or the like)\nIssued to: ftp\nValid: 2026-01-01 to 2027-01-01\nFingerprint: SHA256:abc\n\nTrust this certificate and continue?",
		"10.0.0.1:22 서버는 안전하지 않은 옛 알고리즘만 지원합니다.\n\n키 교환: diffie-hellman-group1-sha1\n\n오래된 스위치·공유기·임베디드 장비에서 흔합니다. 이 서버에 한해 허용하고 접속할까요?": "The server 10.0.0.1:22 only supports old, insecure algorithms.\n\nKey exchange: diffie-hellman-group1-sha1\n\nThis is common with old switches, routers and embedded devices. Allow them for this server only and connect?",
		// a Lua error: the place ("console:1:") and the function ("send:") stay, the message is translated
		"console:1: send: 탭 없이 실행할 때는 send()를 쓸 수 없습니다":     "console:1: send: send() can't be used when running without a tab",
		"오류: console:1: send: 탭 없이 실행할 때는 send()를 쓸 수 없습니다": "Error: console:1: send: send() can't be used when running without a tab",
		"login.lua:12: 연결되어 있지 않습니다":                        "login.lua:12: Not connected",
		"console:2: 사용자 메시지":                                "console:2: 사용자 메시지",
		"사용자 파일 이름.txt":                                     "사용자 파일 이름.txt", // unknown: unchanged
		"plain English":                                     "plain English",
	} {
		if got := enTranslate(in, 0); got != want {
			t.Errorf("%q:\n got %q\nwant %q", in, got, want)
		}
	}
}
