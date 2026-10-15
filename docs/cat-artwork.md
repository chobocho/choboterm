# 고양이 스프라이트 제작·검토

`frontend/src/assets/cat.png`는 8가지 동작, 총 64프레임을 담습니다.
좌표·셀 크기·걷기 이동량은 `frontend/src/catframes.json`에서 관리합니다.
앱은 아틀라스를 0.5배로 표시합니다.

외형 기준은 원본 이미지의 두 번째 줄, 기지개 동작의 서 있는 고양이입니다.
걷기는 원본 영상의 78~101번 프레임을 사용합니다. 다리 순서와 접지 시점을
유지하면서 몸통 길이를 조정합니다. 걷는 얼굴은 영상처럼 두 눈이 잘 보이는 각도를
유지하고, 기준 이미지의 얼굴 크기와 털색에 맞춰 목 움직임을 따라 합성합니다.
얼굴과 몸통의 배율은 전체 24프레임에서 고정합니다. 눈을 뜬 자세는 가까운 눈과
먼 눈을 같은 표시 크기로 보정합니다. 기존 눈을 지우고 같은 눈 그림을 양쪽에
사용해 눈동자와 윤곽선이 겹치지 않도록 하며, 얼굴 밖으로 잘리지 않게 배치합니다.
하품·수면 등 눈을 감는 표정과 완전한 뒷모습은 그대로 유지합니다.

다른 동작도 털·줄무늬·크림색 부위를 구분해 기준 색상에 맞춥니다.
앉거나 서 있는 자세는 높이를 맞추고, 웅크린 수면 프레임의 크기 변화를 줄입니다.
뒤돌아보기와 구르기는 기존 자세를 유지하면서 얼굴을 원본 이미지의 얼굴로 교체합니다.
잠잘 때의 고개 숙임과 걷기의 상하 움직임 등 자세에 따른 변화는 유지합니다.
이미지 생성 API는 사용하지 않습니다.

필요한 도구는 Python, Pillow, numpy, OpenCV와 ffmpeg입니다.
원본은 `고양이 스프라이트 개선.png`(1856×2304),
`고양이 걷기 개선.mp4`(1280×720, 24fps)이며,
별도 파일로 보관합니다. `tools/cat_style.py`의 좌표는 이 원본을 기준으로 합니다.

```powershell
# 수정 전 파일을 보관한 뒤 생성합니다.
New-Item -ItemType Directory -Force output/cat-review
Copy-Item frontend/src/assets/cat.png output/cat-review/before.png
Copy-Item frontend/src/catframes.json output/cat-review/before.json

python tools/make_cat.py "원본 경로/고양이 스프라이트 개선.png" "원본 경로/고양이 걷기 개선.mp4" 78 1 24 output/cat-review
python tools/preview_cat.py output/cat-review/before.png output/cat-review/before.json "원본 경로/고양이 걷기 개선.mp4" output/cat-review

cd frontend
npm run test:e2e -- cat.spec.ts
```

검토 자료는 `output/cat-review`에 생성됩니다.

- `index.html`: 원본 영상과 동기화한 걷기, 동작·속도 선택이 가능한 수정 전후 비교
- `walk-comparison.mp4`, `walk-comparison.gif`: 실제 수정한 24프레임을 이동시키며 3회 반복한 비교
- `all-actions.mp4`, `all-actions.gif`: 전체 동작과 자세 전환 확인
- `all-poses.png`, `walk-24-frames.png`: 같은 배율과 바닥선을 사용하는 프레임 목록
- `frames/`: 앱에 반영한 각 동작의 투명 PNG
- `validation.json`: 프레임 수·아틀라스 범위·접지 이동량·좌우 눈동자 크기 검사 결과

영상과 GIF의 확대 표시는 외형 검토용입니다. 앱의 기지개·수면·점프 재생 시간,
입력 반응, 끌기와 Lua 기능은 브라우저 테스트로 별도 확인합니다.
접지 이동량 검사는 바닥에 닿은 발의 픽셀 범위에서 구한 근사치입니다.
눈을 뜬 43프레임은 최종 아틀라스에서 양쪽 눈동자의 가로·세로 크기를 검사합니다.
래스터 축소로 생기는 차이는 아틀라스 1px, 앱 표시 크기 0.5px까지 허용합니다.
