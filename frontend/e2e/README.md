# End-to-end tests of the page

These tests drive the real page in a browser: each test starts its own
headless choboterm (the web server without the window, `-tags headless`) and
opens its page in Edge.

    cd frontend
    npm install
    npm run test:e2e

- `npm run test:e2e` builds the page (`npm run build`) and the headless
  program first. `E2E_SKIP_BUILD=1` reuses the last build, which is enough
  when only the tests changed.
- `npx playwright test tabs` runs one file, `-g "font size"` the tests whose
  name matches, `--headed` shows the browser, `--repeat-each 3` looks for
  flaky tests.
- `E2E_BROWSER=chrome` uses Chrome instead of Edge. No browser is downloaded.

## What a test runs against

- Its own program on its own port, with its own settings, history, sessions
  and home folder under `e2e/.tmp/results/`: nothing of the user's is read or
  changed, and a choboterm that is already running is left alone. The program
  is stopped by its own process id when the test ends.
- A shell on this PC (`cmd`) for terminal input and output, and the tests'
  own TCP server (`FakeServer` in `fixtures.ts`, taken for Telnet) for what
  needs a server: labels, the encoding, a connection that ends or breaks.
- The browser runs without a GPU, so terminals are drawn with the DOM
  renderer and their text can be read (`app.screen`).

## What is not covered

- SSH, FTP, serial ports and docker (they need real servers or devices),
  and so the file window, port forwarding and the containers' tabs.
- The PC's clipboard (copy on select, right-click paste): a test would
  overwrite what the user copied. Pasting is tested with a paste event.
- Windows written for the native window only (translucency, the web server's
  settings), and the touch selection of the Termux build.

The shell commands in the tests are `cmd` ones, so the suite is for Windows.
