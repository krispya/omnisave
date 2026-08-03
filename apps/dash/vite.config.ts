import { defineConfig } from 'vite';
import react, { reactCompilerPreset } from '@vitejs/plugin-react';
import babel from '@rolldown/plugin-babel';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  // Dash routes are real paths, so assets must resolve from the root rather than from
  // whatever route the browser happens to be on.
  base: '/',
  publicDir: '../../assets/icons',
  plugins: [react(), babel({ presets: [reactCompilerPreset()] }), tailwindcss()],
  server: {
    proxy: {
      '/api': process.env.OMNISAVE_DEV_API ?? 'http://localhost:8080',
    },
  },
});
