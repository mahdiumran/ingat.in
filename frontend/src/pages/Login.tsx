import { useEffect, useRef, useState } from 'react'
import { api, ApiUser, setSession } from '../api'

/**
 * Login page.
 *
 * Layout mengikuti pola m2c: panel kiri dengan animasi network canvas,
 * panel kanan berisi form. Animasi diadaptasi dari m2c/js/canvas.js —
 * subtle, tanpa partikel berlebihan, DPR dibatasi 1.5.
 *
 * Terhubung ke POST /api/auth/login (F2). Menampilkan pesan kesalahan yang
 * spesifik untuk 401 (kredensial salah), 403 (akun nonaktif), dan 429.
 */
export default function Login({
  onAuthenticated,
}: {
  onAuthenticated: (user: ApiUser) => void
}) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [remember, setRemember] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      const res = await api.login(username.trim(), password)
      // Bila "ingat saya" tidak dicentang, sesi tetap disimpan tetapi
      // refresh token ditandai agar dibersihkan saat tab ditutup.
      setSession(res.access_token, res.refresh_token, res.user)
      if (!remember) {
        sessionStorage.setItem('ingatin_ephemeral_session', '1')
      }
      onAuthenticated(res.user)
    } catch (err) {
      const status = (err as { status?: number }).status
      if (status === 401) {
        setError('Username atau password salah.')
      } else if (status === 403) {
        setError('Akun Anda tidak aktif. Hubungi administrator.')
      } else if (status === 429) {
        setError('Terlalu banyak percobaan login. Coba lagi nanti.')
      } else {
        setError(err instanceof Error ? err.message : 'Login gagal')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen flex-col lg:flex-row">
      {/* ---------------- Panel kiri: visual ---------------- */}
      <section className="relative flex min-h-[240px] flex-1 items-end overflow-hidden bg-inverse-surface lg:min-h-screen lg:items-center">
        <NetworkCanvas />
        <div className="relative z-10 w-full px-6 py-10 lg:px-12">
          <div className="mb-4 flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-lg border border-accent-soft/50 bg-accent-brand/15">
              <span className="material-symbols-outlined text-[24px] text-accent-brand">
                notifications_active
              </span>
            </div>
            <div className="flex flex-col">
              <span className="font-headline text-xl font-bold leading-tight text-inverse-on-surface">
                Ingat.in
              </span>
              <span className="text-label-sm uppercase tracking-widest text-inverse-on-surface/60">
                NOC Reminder Hub
              </span>
            </div>
          </div>
          <h2 className="max-w-md font-headline text-2xl font-semibold leading-snug text-inverse-on-surface lg:text-headline-lg">
            Tidak ada lagi trial atau RFS yang terlewat.
          </h2>
          <p className="mt-3 max-w-md text-body-sm text-inverse-on-surface/70">
            Todo task, reminder trial dedicated, dan pengingat RFS — terkirim otomatis ke
            Telegram dan WhatsApp untuk tim NOC.
          </p>
          <div className="mt-6 flex flex-wrap gap-x-5 gap-y-2 text-label-sm uppercase tracking-widest text-inverse-on-surface/50">
            <span className="inline-flex items-center gap-1.5">
              <span className="material-symbols-outlined text-[15px]">task_alt</span>Todo
            </span>
            <span className="inline-flex items-center gap-1.5">
              <span className="material-symbols-outlined text-[15px]">alarm</span>Reminder
            </span>
            <span className="inline-flex items-center gap-1.5">
              <span className="material-symbols-outlined text-[15px]">event_available</span>RFS
            </span>
          </div>
        </div>
      </section>

      {/* ---------------- Panel kanan: form ---------------- */}
      <section className="flex flex-1 items-center justify-center bg-background px-6 py-12">
        <div className="w-full max-w-sm">
          <div className="kicker">Masuk</div>
          <h1 className="mt-1 font-headline text-headline-md font-semibold">
            Selamat datang kembali
          </h1>
          <p className="mt-1.5 text-body-sm text-text-secondary">
            Masuk untuk mengelola todo, reminder, dan data RFS.
          </p>

          <form onSubmit={submit} className="mt-7 space-y-4">
            <div>
              <label className="label-field" htmlFor="username">
                Username
              </label>
              <input
                id="username"
                className="input"
                autoComplete="username"
                autoFocus
                required
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="admin"
              />
            </div>

            <div>
              <label className="label-field" htmlFor="password">
                Password
              </label>
              <input
                id="password"
                className="input"
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
              />
            </div>

            <label className="flex cursor-pointer items-center gap-2 text-body-sm text-text-secondary">
              <input
                type="checkbox"
                className="h-4 w-4 rounded border-border text-primary focus:ring-primary/30"
                checked={remember}
                onChange={(e) => setRemember(e.target.checked)}
              />
              Ingat saya di perangkat ini
            </label>

            {error && (
              <div
                role="alert"
                className="flex gap-2 rounded-control border border-critical/30 bg-critical-container/60 p-3 text-body-sm text-on-critical-container"
              >
                <span className="material-symbols-outlined text-[18px] shrink-0">error</span>
                <span>{error}</span>
              </div>
            )}

            <button type="submit" className="btn-primary w-full" disabled={busy}>
              {busy ? (
                <>
                  <span className="material-symbols-outlined animate-spin text-[18px]">
                    progress_activity
                  </span>
                  Memproses…
                </>
              ) : (
                <>
                  <span className="material-symbols-outlined text-[18px]">login</span>
                  Masuk
                </>
              )}
            </button>
          </form>

          <div className="mt-6 flex items-center justify-center gap-4 border-t border-border pt-4 text-label-sm uppercase tracking-widest text-text-secondary">
            <span className="inline-flex items-center gap-1">
              <span className="material-symbols-outlined text-[14px]">shield</span>Aman
            </span>
            <span className="inline-flex items-center gap-1">
              <span className="material-symbols-outlined text-[15px]">history</span>Teraudit
            </span>
            <span className="inline-flex items-center gap-1">
              <span className="material-symbols-outlined text-[15px]">badge</span>Berbasis peran
            </span>
          </div>
        </div>
      </section>
    </div>
  )
}

/**
 * NetworkCanvas menggambar jaringan node bergerak yang halus.
 * Adaptasi ringkas dari m2c/js/canvas.js: partikel + garis penghubung +
 * interaksi mouse, dengan kepadatan rendah agar tidak mengganggu.
 */
function NetworkCanvas() {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    let width = 0
    let height = 0
    let dpr = 1
    let frame = 0
    const mouse = { x: null as number | null, y: null as number | null, radius: 140 }

    type Particle = { x: number; y: number; vx: number; vy: number; r: number }
    let particles: Particle[] = []

    // Hormati preferensi "kurangi gerakan".
    const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches

    function initParticles() {
      const count = Math.min(46, Math.floor((width * height) / 26000))
      particles = Array.from({ length: count }, () => ({
        x: Math.random() * width,
        y: Math.random() * height,
        vx: (Math.random() - 0.5) * 0.24,
        vy: (Math.random() - 0.5) * 0.24,
        r: Math.random() * 1.6 + 0.7,
      }))
    }

    function resize() {
      const rect = canvas!.getBoundingClientRect()
      width = rect.width
      height = rect.height
      dpr = Math.min(window.devicePixelRatio || 1, 1.5)
      canvas!.width = Math.floor(width * dpr)
      canvas!.height = Math.floor(height * dpr)
      ctx!.setTransform(dpr, 0, 0, dpr, 0, 0)
      mouse.radius = width < 640 ? 100 : 160
      initParticles()
    }

    function draw() {
      ctx!.clearRect(0, 0, width, height)

      for (const p of particles) {
        p.x += p.vx
        p.y += p.vy
        if (p.x < 0 || p.x > width) p.vx *= -1
        if (p.y < 0 || p.y > height) p.vy *= -1
      }

      // Garis penghubung antar partikel yang berdekatan.
      for (let i = 0; i < particles.length; i++) {
        for (let j = i + 1; j < particles.length; j++) {
          const a = particles[i]
          const b = particles[j]
          const dx = a.x - b.x
          const dy = a.y - b.y
          const dist = Math.hypot(dx, dy)
          if (dist < 118) {
            const alpha = (1 - dist / 118) * 0.22
            ctx!.strokeStyle = `rgba(74, 222, 128, ${alpha})`
            ctx!.lineWidth = 0.7
            ctx!.beginPath()
            ctx!.moveTo(a.x, a.y)
            ctx!.lineTo(b.x, b.y)
            ctx!.stroke()
          }
        }
      }

      // Interaksi mouse: garis dari kursor ke partikel terdekat.
      if (mouse.x !== null && mouse.y !== null) {
        for (const p of particles) {
          const dist = Math.hypot(p.x - mouse.x, p.y - mouse.y)
          if (dist < mouse.radius) {
            const alpha = (1 - dist / mouse.radius) * 0.3
            ctx!.strokeStyle = `rgba(187, 247, 208, ${alpha})`
            ctx!.lineWidth = 0.8
            ctx!.beginPath()
            ctx!.moveTo(p.x, p.y)
            ctx!.lineTo(mouse.x, mouse.y)
            ctx!.stroke()
          }
        }
      }

      // Partikel.
      for (const p of particles) {
        ctx!.fillStyle = 'rgba(74, 222, 128, 0.55)'
        ctx!.beginPath()
        ctx!.arc(p.x, p.y, p.r, 0, Math.PI * 2)
        ctx!.fill()
      }

      if (!reduceMotion) frame = requestAnimationFrame(draw)
    }

    function onMove(e: MouseEvent) {
      const rect = canvas!.getBoundingClientRect()
      mouse.x = e.clientX - rect.left
      mouse.y = e.clientY - rect.top
    }
    function onLeave() {
      mouse.x = null
      mouse.y = null
    }

    resize()
    draw()

    window.addEventListener('resize', resize)
    canvas.addEventListener('mousemove', onMove)
    canvas.addEventListener('mouseleave', onLeave)

    return () => {
      cancelAnimationFrame(frame)
      window.removeEventListener('resize', resize)
      canvas.removeEventListener('mousemove', onMove)
      canvas.removeEventListener('mouseleave', onLeave)
    }
  }, [])

  return <canvas ref={canvasRef} className="absolute inset-0 h-full w-full" aria-hidden />
}
