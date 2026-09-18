import { useCallback, useEffect, useRef, useState } from 'react'
import { masterDataApi } from './api'

/**
 * useAsync memuat data sekali (dan dapat dimuat ulang) dengan state eksplisit:
 * loading / success / error. Setiap halaman wajib menampilkan ketiganya.
 */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // Simpan fn terbaru agar reload tidak bergantung pada identitas fungsi.
  const fnRef = useRef(fn)
  fnRef.current = fn

  const reload = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const result = await fnRef.current()
      setData(result)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Gagal memuat data')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    let alive = true
    setLoading(true)
    setError('')
    fnRef
      .current()
      .then((result) => {
        if (alive) setData(result)
      })
      .catch((err) => {
        if (alive) setError(err instanceof Error ? err.message : 'Gagal memuat data')
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return { data, loading, error, reload, setData }
}

/** useDebounced mengembalikan nilai yang tertunda — untuk kolom pencarian. */
export function useDebounced<T>(value: T, delay = 350): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(value), delay)
    return () => window.clearTimeout(t)
  }, [value, delay])
  return debounced
}

/**
 * useMasterDataKind memuat entri satu kelompok master data (mis.
 * `incident_type`, `ticket_category`, `tag_color`) untuk mengisi dropdown.
 */
export function useMasterDataKind(kind: string) {
  return useAsync(() => masterDataApi.list({ kind }), [kind])
}

/**
 * useTagColors memuat peta nama tag → warna dari Master Data `tag_color`.
 * Setiap entri menyimpan warna di `meta.color`.
 */
export function useTagColors(): Record<string, string> {
  const data = useAsync(() => masterDataApi.list({ kind: 'tag_color' }), [])
  const out: Record<string, string> = {}
  for (const e of data.data?.entries ?? []) {
    const color = (e.meta as { color?: string } | undefined)?.color
    if (e.code && color) {
      out[e.code] = color
      out[e.code.toLowerCase()] = color
    }
  }
  return out
}

