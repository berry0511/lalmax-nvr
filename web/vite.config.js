import { defineConfig } from 'vitest/config'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import fs from 'node:fs'
import path from 'path'

function ortWasmAssetsPlugin() {
  return {
    name: 'ort-wasm-assets',
    apply: 'build',
    closeBundle() {
      const ortDist = path.resolve('node_modules/onnxruntime-web/dist')
      const ortOut = path.resolve('dist/ort')
      fs.mkdirSync(ortOut, { recursive: true })
      for (const file of [
        'ort.all.bundle.min.mjs',
        'ort.wasm.bundle.min.mjs',
        'ort-wasm-simd-threaded.jsep.mjs',
        'ort-wasm-simd-threaded.jsep.wasm',
        'ort-wasm-simd-threaded.wasm',
      ]) {
        const source = path.join(ortDist, file)
        if (!fs.existsSync(source)) throw new Error(`Missing ONNX Runtime asset: ${source}`)
        fs.copyFileSync(source, path.join(ortOut, file))
      }
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte(), tailwindcss(), ortWasmAssetsPlugin()],
  resolve: {
    alias: {
      $lib: path.resolve('./src/lib'),
    },
    extensions: ['.js', '.ts', '.svelte', '.svelte.ts'],
    conditions: ['browser'],
  },
  build: {
    // App entry is ~875 kB after vendor chunks are split out.
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/chart.js')) {
            return 'vendor-chart';
          }
          if (id.includes('node_modules/svelte')) {
            return 'vendor-svelte';
          }
          if (id.includes('node_modules/hls.js')) {
            return 'vendor-hls';
          }
          if (id.includes('node_modules/lucide-svelte')) {
            return 'vendor-lucide';
          }
          if (id.includes('node_modules/onnxruntime-web')) {
            return 'vendor-onnx';
          }
          if (id.includes('node_modules')) {
            return 'vendor';
          }
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
  },
})
