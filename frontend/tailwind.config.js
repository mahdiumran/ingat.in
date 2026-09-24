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
        // ---- Token tema (F28) ----
        // Dipetakan ke CSS variable (format "R G B") dari src/styles.css agar
        // Light/Dark dapat berganti tanpa mengubah komponen.
        primary: 'rgb(var(--primary) / <alpha-value>)',
        'on-primary': 'rgb(var(--on-primary) / <alpha-value>)',
        'primary-container': 'rgb(var(--primary-container) / <alpha-value>)',
        'on-primary-container': 'rgb(var(--on-primary-container) / <alpha-value>)',
        'primary-fixed': 'rgb(var(--primary-fixed) / <alpha-value>)',
        'primary-fixed-dim': 'rgb(var(--primary-fixed-dim) / <alpha-value>)',
        'on-primary-fixed': 'rgb(var(--on-primary-fixed) / <alpha-value>)',
        'on-primary-fixed-variant': 'rgb(var(--on-primary-fixed-variant) / <alpha-value>)',
        'inverse-primary': 'rgb(var(--inverse-primary) / <alpha-value>)',
        'surface-tint': 'rgb(var(--surface-tint) / <alpha-value>)',

        secondary: 'rgb(var(--secondary) / <alpha-value>)',
        'on-secondary': 'rgb(var(--on-secondary) / <alpha-value>)',
        'secondary-container': 'rgb(var(--secondary-container) / <alpha-value>)',
        'on-secondary-container': 'rgb(var(--on-secondary-container) / <alpha-value>)',

        tertiary: 'rgb(var(--tertiary) / <alpha-value>)',
        'on-tertiary': 'rgb(var(--on-tertiary) / <alpha-value>)',
        'tertiary-container': 'rgb(var(--tertiary-container) / <alpha-value>)',
        'on-tertiary-container': 'rgb(var(--on-tertiary-container) / <alpha-value>)',

        background: 'rgb(var(--background) / <alpha-value>)',
        'on-background': 'rgb(var(--on-background) / <alpha-value>)',
        surface: 'rgb(var(--surface) / <alpha-value>)',
        'surface-dim': 'rgb(var(--surface-dim) / <alpha-value>)',
        'surface-bright': 'rgb(var(--surface-bright) / <alpha-value>)',
        'surface-container-lowest': 'rgb(var(--surface-container-lowest) / <alpha-value>)',
        'surface-container-low': 'rgb(var(--surface-container-low) / <alpha-value>)',
        'surface-container': 'rgb(var(--surface-container) / <alpha-value>)',
        'surface-container-high': 'rgb(var(--surface-container-high) / <alpha-value>)',
        'surface-container-highest': 'rgb(var(--surface-container-highest) / <alpha-value>)',
        'surface-variant': 'rgb(var(--surface-variant) / <alpha-value>)',
        'on-surface': 'rgb(var(--on-surface) / <alpha-value>)',
        'on-surface-variant': 'rgb(var(--on-surface-variant) / <alpha-value>)',
        'inverse-surface': 'rgb(var(--inverse-surface) / <alpha-value>)',
        'inverse-on-surface': 'rgb(var(--inverse-on-surface) / <alpha-value>)',

        outline: 'rgb(var(--outline) / <alpha-value>)',
        'outline-variant': 'rgb(var(--outline-variant) / <alpha-value>)',
        border: 'rgb(var(--border) / <alpha-value>)',

        error: 'rgb(var(--error) / <alpha-value>)',
        'on-error': 'rgb(var(--on-error) / <alpha-value>)',
        'error-container': 'rgb(var(--error-container) / <alpha-value>)',
        'on-error-container': 'rgb(var(--on-error-container) / <alpha-value>)',

        'text-primary': 'rgb(var(--text-primary) / <alpha-value>)',
        'text-secondary': 'rgb(var(--text-secondary) / <alpha-value>)',

        // ---- Token semantik Ingat.in ----
        success: 'rgb(var(--success) / <alpha-value>)',
        'success-container': 'rgb(var(--success-container) / <alpha-value>)',
        'on-success-container': 'rgb(var(--on-success-container) / <alpha-value>)',
        'accent-brand': 'rgb(var(--accent-brand) / <alpha-value>)',
        'accent-soft': 'rgb(var(--accent-soft) / <alpha-value>)',
        warning: 'rgb(var(--warning) / <alpha-value>)',
        'warning-container': 'rgb(var(--warning-container) / <alpha-value>)',
        'on-warning-container': 'rgb(var(--on-warning-container) / <alpha-value>)',
        critical: 'rgb(var(--critical) / <alpha-value>)',
        'critical-container': 'rgb(var(--critical-container) / <alpha-value>)',
        'on-critical-container': 'rgb(var(--on-critical-container) / <alpha-value>)',
        info: 'rgb(var(--info) / <alpha-value>)',
        'info-container': 'rgb(var(--info-container) / <alpha-value>)',
        'on-info-container': 'rgb(var(--on-info-container) / <alpha-value>)',
        unknown: 'rgb(var(--unknown) / <alpha-value>)',
        topbar: 'rgb(var(--topbar) / <alpha-value>)',
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
        // Intensitas shadow dikendalikan CSS var (lebih pekat di dark).
        card: 'var(--shadow-card)',
        raised: 'var(--shadow-raised)',
        modal: 'var(--shadow-modal)',
      },

      transitionDuration: {
        DEFAULT: '150ms',
      },
    },
  },
  plugins: [],
}
