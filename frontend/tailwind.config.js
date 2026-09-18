/**
 * Preset Tailwind Ingat.in — diturunkan langsung dari m2c/tailwind.config.js
 * (M2Cloud) agar bahasa visual konsisten.
 *
 * Perubahan terhadap m2c:
 *  - ditambahkan token semantik status (success/warning/critical) yang
 *    dipetakan ke palet m2c + amber/merah standar
 *  - fontFamily diberi nama tingkat tinggi (headline/body/label) sekaligus
 *    nama spesifik agar kompatibel dengan markup m2c
 *
 * Token warna/font TIDAK boleh di-hard-code di komponen; gunakan kelas di sini
 * atau lihat src/design/tokens.ts.
 */

/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // ---- m2c palette (verbatim) ----
        primary: '#006d36',
        'on-primary': '#ffffff',
        'primary-container': '#4ade80',
        'on-primary-container': '#005e2d',
        'primary-fixed': '#6dfe9c',
        'primary-fixed-dim': '#4de082',
        'on-primary-fixed': '#00210c',
        'on-primary-fixed-variant': '#005227',
        'inverse-primary': '#4de082',
        'surface-tint': '#006d36',

        secondary: '#5d5f5f',
        'on-secondary': '#ffffff',
        'secondary-container': '#dfe0e0',
        'on-secondary-container': '#616363',
        'secondary-fixed': '#e2e2e2',
        'secondary-fixed-dim': '#c6c6c7',
        'on-secondary-fixed': '#1a1c1c',
        'on-secondary-fixed-variant': '#454747',

        tertiary: '#31694b',
        'on-tertiary': '#ffffff',
        'tertiary-container': '#97d1ac',
        'on-tertiary-container': '#235b3e',
        'tertiary-fixed': '#b4f0c9',
        'tertiary-fixed-dim': '#99d4ae',
        'on-tertiary-fixed': '#002111',
        'on-tertiary-fixed-variant': '#175034',

        background: '#f9f9ff',
        'on-background': '#141b2b',
        surface: '#f9f9ff',
        'surface-dim': '#d3daef',
        'surface-bright': '#f9f9ff',
        'surface-container-lowest': '#ffffff',
        'surface-container-low': '#f1f3ff',
        'surface-container': '#e9edff',
        'surface-container-high': '#e1e8fd',
        'surface-container-highest': '#dce2f7',
        'surface-variant': '#dce2f7',
        'on-surface': '#141b2b',
        'on-surface-variant': '#3d4a3e',
        'inverse-surface': '#293040',
        'inverse-on-surface': '#edf0ff',

        outline: '#6d7b6d',
        'outline-variant': '#bccabb',
        border: '#E5E7EB',

        error: '#ba1a1a',
        'on-error': '#ffffff',
        'error-container': '#ffdad6',
        'on-error-container': '#93000a',

        'text-primary': '#111827',
        'text-secondary': '#4B5563',

        // ---- Token semantik Ingat.in (dipetakan dari palet m2c) ----
        // Diambil dari token m2c + standar amber untuk warning.
        success: '#006d36',
        'success-container': '#b4f0c9',
        'on-success-container': '#002111',
        'accent-brand': '#4ADE80', // brand/CTA m2c (#4ADE80)
        'accent-soft': '#BBF7D0', // chip/border m2c
        warning: '#b45309',
        'warning-container': '#fde68a',
        'on-warning-container': '#78350f',
        critical: '#ba1a1a',
        'critical-container': '#ffdad6',
        'on-critical-container': '#93000a',
        info: '#31694b',
        'info-container': '#97d1ac',
        'on-info-container': '#235b3e',
        unknown: '#6d7b6d',
      },

      borderRadius: {
        DEFAULT: '0.25rem',
        sm: '0.25rem',
        md: '0.375rem',
        lg: '0.5rem',
        xl: '0.75rem',
        '2xl': '1rem',
        card: '0.5rem',
        control: '0.5rem',
        modal: '0.75rem',
        full: '9999px',
      },

      spacing: {
        'space-xs': '0.25rem',
        'space-sm': '0.5rem',
        'space-md': '1rem',
        'space-lg': '1.5rem',
        'space-xl': '5rem',
        base: '8px',
        gap: '16px',
        'card-padding': '24px',
        'section-padding': '80px',
        gutter: '1rem',
        margin: '1.5rem',
        'margin-mobile': '1rem',
        'margin-desktop': '5rem',
        'sidebar-expanded': '232px',
        'sidebar-collapsed': '72px',
        topbar: '64px',
      },

      fontFamily: {
        // Nama tingkat tinggi
        headline: ['Inter', 'sans-serif'],
        display: ['Inter', 'sans-serif'],
        body: ['Space Grotesk', 'sans-serif'],
        label: ['JetBrains Mono', 'monospace'],
        // Nama spesifik m2c (kompatibilitas markup)
        'display-lg': ['Inter', 'sans-serif'],
        'display-lg-mobile': ['Inter', 'sans-serif'],
        'headline-lg': ['Inter', 'sans-serif'],
        'headline-md': ['Inter', 'sans-serif'],
        'headline-sm': ['Inter', 'sans-serif'],
        'body-lg': ['Space Grotesk', 'sans-serif'],
        'body-md': ['Space Grotesk', 'sans-serif'],
        'body-sm': ['Space Grotesk', 'sans-serif'],
        'label-lg': ['JetBrains Mono', 'monospace'],
        'label-md': ['JetBrains Mono', 'monospace'],
        'label-sm': ['JetBrains Mono', 'monospace'],
      },

      fontSize: {
        'display-lg': ['56px', { lineHeight: '1.08', letterSpacing: '-0.03em', fontWeight: '800' }],
        'display-lg-mobile': ['36px', { lineHeight: '40px', letterSpacing: '-0.02em', fontWeight: '700' }],
        'headline-lg': ['32px', { lineHeight: '38px', fontWeight: '700' }],
        'headline-md': ['24px', { lineHeight: '30px', fontWeight: '600' }],
        'headline-sm': ['20px', { lineHeight: '26px', fontWeight: '600' }],
        'body-lg': ['18px', { lineHeight: '28px', fontWeight: '400' }],
        'body-md': ['16px', { lineHeight: '1.6', fontWeight: '400' }],
        'body-sm': ['14px', { lineHeight: '22px', fontWeight: '400' }],
        'label-lg': ['14px', { lineHeight: '18px', fontWeight: '600' }],
        'label-md': ['12px', { lineHeight: '1.2', fontWeight: '600' }],
        'label-sm': ['10px', { lineHeight: '12px', fontWeight: '600' }],
      },

      boxShadow: {
        // m2c memakai shadow tipis; shadow-sm/md dari Tailwind sudah cocok.
        card: '0 1px 2px 0 rgb(0 0 0 / 0.05)',
        raised: '0 4px 6px -1px rgb(0 0 0 / 0.08), 0 2px 4px -2px rgb(0 0 0 / 0.06)',
        modal: '0 20px 25px -5px rgb(0 0 0 / 0.10), 0 8px 10px -6px rgb(0 0 0 / 0.10)',
      },

      transitionDuration: {
        DEFAULT: '150ms',
      },
    },
  },
  plugins: [],
}
