import { defineConfig } from 'vite'

// 开发环境:Vite 提供全部前端(含热更新),/api /p/ /s/ 代理到 nasd。
// 生产环境:构建产物输出到 ../internal/webui/dist,由 nasd go:embed 托管。
export default defineConfig({
  server: {
    port: 5173,
    // 代理保持 changeOrigin=false:Host 仍为 5173,与浏览器发出的 Origin 一致,
    // 后端 checkSameOrigin(Origin==Host)校验天然通过,无需额外处理。
    proxy: {
      '/api': { target: 'http://localhost:7531' },
      '/p': { target: 'http://localhost:7531' },
      '/s': { target: 'http://localhost:7531' },
      '/health': { target: 'http://localhost:7531' }
    }
  },
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    sourcemap: false
  }
})
