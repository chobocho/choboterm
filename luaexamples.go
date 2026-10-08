package main

// luaExamples are written to a new scripts folder, as a start.
var luaExamples = map[string]string{
	"예제_자동로그인.lua": `-- 자동 로그인 예제 (Telnet 장비 등)
-- expect(패턴, 초): 출력에 패턴이 나올 때까지 기다림. 시간이 지나면 nil
-- send(문자열): 키보드로 입력한 것처럼 보냄 ("\r" = Enter)

if not expect("login: ", 10) then
    error("login: 프롬프트가 나오지 않았습니다")
end
send("admin\r")
expect("Password: ", 10)
send("비밀번호\r")
expect("[#>$] ?$", 10)  -- 프롬프트
print("로그인 완료")
`,
	"예제_반복명령.lua": `-- 같은 명령을 간격을 두고 반복하는 예제
-- sleep(밀리초), print(...): 탭에 노란 글씨로 표시

for i = 1, 5 do
    send("date\r")
    expect("%d%d:%d%d:%d%d", 5)  -- 시각이 출력될 때까지
    print(i .. "번째 완료")
    sleep(2000)
end
`,
	"예제_화면읽기.lua": `-- screen(): 지금 화면에 보이는 글자
-- Lua 5.1 문법입니다 (gopher-lua).

local text = screen()
local n = 0
for _ in text:gmatch("error") do n = n + 1 end
print("화면에 error가 " .. n .. "번 있습니다")
`,
}
