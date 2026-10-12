import {execSync} from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import {EXE} from './fixtures';

// Builds the page and the headless program the tests run. E2E_SKIP_BUILD=1
// reuses the last build (quicker when only the tests changed).
export default function globalSetup() {
    if (process.env.E2E_SKIP_BUILD === '1' && fs.existsSync(EXE)) return;
    const frontend = path.join(__dirname, '..');
    const repo = path.join(frontend, '..');
    execSync('npm run build', {cwd: frontend, stdio: 'inherit'});
    fs.mkdirSync(path.dirname(EXE), {recursive: true});
    // The page (frontend/dist) is embedded into the program.
    execSync(`go build -tags headless -o "${EXE}" .`, {cwd: repo, stdio: 'inherit'});
}
