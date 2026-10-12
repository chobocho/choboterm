import {defineConfig} from 'vite';

export default defineConfig({
    build: {
        rollupOptions: {
            output: {
                manualChunks(id) {
                    if (!id.includes('/node_modules/')) return;

                    if (id.includes('/@codemirror/') || id.includes('/@lezer/')) return 'codemirror';
                    if (id.includes('/@xterm/addon-webgl/')) return 'xterm-webgl';
                    if (id.includes('/@xterm/')) return 'xterm';
                },
            },
        },
    },
});
