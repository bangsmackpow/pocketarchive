import * as React from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  CheckCircle2,
  Clock,
  FileArchive,
  FileText,
  RotateCw,
  UploadCloud,
  XCircle,
} from 'lucide-react'
import { apiFetch, uploadChunk } from '~/lib/api'
import { cn, formatBytes, formatDateTime, relativeTime } from '~/lib/format'
import type { ImportJob, ImportJobError } from '~/lib/types'
import {
  Badge,
  Button,
  Card,
  CardHeader,
  EmptyState,
  Modal,
  ProgressBar,
  Spinner,
} from '~/components/ui'

export const Route = createFileRoute('/_app/imports')({
  component: ImportsPage,
})

const CHUNK_SIZE = 8 * 1024 * 1024

function statusTone(status: ImportJob['status']): 'emerald' | 'amber' | 'rose' | 'neutral' | 'indigo' {
  if (status === 'completed') return 'emerald'
  if (status === 'failed') return 'rose'
  if (status === 'running' || status === 'queued') return 'amber'
  if (status === 'awaiting_upload') return 'indigo'
  return 'neutral'
}

function ImportsPage() {
  const queryClient = useQueryClient()
  const [file, setFile] = React.useState<File | null>(null)
  const [dragging, setDragging] = React.useState(false)
  const [uploading, setUploading] = React.useState(false)
  const [uploadLabel, setUploadLabel] = React.useState('')
  const [uploadedBytes, setUploadedBytes] = React.useState(0)
  const [uploadTotal, setUploadTotal] = React.useState(0)
  const [uploadError, setUploadError] = React.useState('')
  const [errorJob, setErrorJob] = React.useState<ImportJob | null>(null)
  const inputRef = React.useRef<HTMLInputElement>(null)

  const jobs = useQuery({
    queryKey: ['imports'],
    queryFn: () => apiFetch<{ jobs: ImportJob[] }>('/imports?limit=50'),
    refetchInterval: uploading ? 1500 : 4000,
  })

  const errors = useQuery({
    queryKey: ['import-errors', errorJob?.id],
    queryFn: () =>
      apiFetch<{ errors: ImportJobError[] }>(`/imports/${errorJob?.id}/errors`),
    enabled: Boolean(errorJob),
  })

  const jobList = jobs.data?.jobs ?? []
  const active = jobList.filter((j) => j.status === 'running' || j.status === 'queued').length
  const completed = jobList.filter((j) => j.status === 'completed').length
  const failed = jobList.filter((j) => j.status === 'failed').length

  function choose(next: File | null) {
    if (!next) return
    const lower = next.name.toLowerCase()
    if (!lower.endsWith('.pst') && !lower.endsWith('.mbox')) {
      setUploadError('Choose a .pst or .mbox archive file.')
      return
    }
    setUploadError('')
    setFile(next)
  }

  async function startUpload() {
    if (!file || uploading) return
    setUploading(true)
    setUploadError('')
    setUploadedBytes(0)
    setUploadTotal(file.size)
    setUploadLabel('Creating import job…')
    try {
      const job = await apiFetch<ImportJob>('/imports', {
        method: 'POST',
        body: JSON.stringify({ filename: file.name, size_bytes: file.size }),
      })
      const totalChunks = Math.max(1, Math.ceil(file.size / CHUNK_SIZE))
      for (let index = 0; index < totalChunks; index++) {
        const start = index * CHUNK_SIZE
        const chunk = file.slice(start, Math.min(file.size, start + CHUNK_SIZE))
        setUploadLabel(`Uploading chunk ${index + 1} of ${totalChunks}`)
        await uploadChunk(`/imports/${job.id}/chunks/${index}`, chunk, (loaded) => {
          setUploadedBytes(start + loaded)
        })
      }
      setUploadLabel('Queueing archive for processing…')
      await apiFetch(`/imports/${job.id}/complete`, { method: 'POST' })
      setFile(null)
      setUploadedBytes(file.size)
      await queryClient.invalidateQueries({ queryKey: ['imports'] })
    } catch (err) {
      setUploadError(err instanceof Error ? err.message : 'Upload failed')
    } finally {
      setUploading(false)
      setUploadLabel('')
    }
  }

  return (
    <div className="mx-auto max-w-[1400px]">
      <div className="mb-5">
        <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-indigo-600">
          Bulk ingestion
        </p>
        <h2 className="mt-1 text-xl font-semibold tracking-tight text-slate-900">
          Import PST and mbox archives
        </h2>
        <p className="mt-1 text-xs text-slate-500">
          Uploads are processed into encrypted, deduplicated messages. Imports can be monitored live and failures are quarantined per item.
        </p>
      </div>

      <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Metric label="Active jobs" value={active} icon={<Clock className="h-4 w-4" />} tone="amber" />
        <Metric label="Completed" value={completed} icon={<CheckCircle2 className="h-4 w-4" />} tone="emerald" />
        <Metric label="Failed" value={failed} icon={<XCircle className="h-4 w-4" />} tone="rose" />
        <Metric
          label="Messages processed"
          value={jobList.reduce((n, j) => n + j.processed_items, 0)}
          icon={<FileArchive className="h-4 w-4" />}
          tone="indigo"
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-[390px_minmax(0,1fr)]">
        <Card className="h-fit">
          <CardHeader
            title="Upload archive"
            description="Chunked 8 MB uploads. PST extraction uses readpst inside the engine container."
          />
          <div className="p-4">
            <div
              onDragOver={(e) => {
                e.preventDefault()
                setDragging(true)
              }}
              onDragLeave={() => setDragging(false)}
              onDrop={(e) => {
                e.preventDefault()
                setDragging(false)
                choose(e.dataTransfer.files[0] ?? null)
              }}
              onClick={() => inputRef.current?.click()}
              className={cn(
                'flex cursor-pointer flex-col items-center justify-center rounded-xl border-2 border-dashed px-5 py-10 text-center transition',
                dragging
                  ? 'border-indigo-400 bg-indigo-50'
                  : 'border-slate-200 bg-slate-50/60 hover:border-indigo-300 hover:bg-indigo-50/30',
              )}
            >
              <div className="flex h-11 w-11 items-center justify-center rounded-full bg-white text-indigo-500 shadow-sm ring-1 ring-slate-200">
                <UploadCloud className="h-5 w-5" />
              </div>
              <p className="mt-3 text-sm font-medium text-slate-800">
                {file ? file.name : 'Drop a .pst or .mbox file here'}
              </p>
              <p className="mt-1 text-xs text-slate-500">
                {file ? formatBytes(file.size) : 'or click to browse'}
              </p>
              <input
                ref={inputRef}
                type="file"
                accept=".pst,.mbox"
                className="hidden"
                onChange={(e) => choose(e.target.files?.[0] ?? null)}
              />
            </div>

            {uploadError && (
              <div className="mt-3 flex items-start gap-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700">
                <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                {uploadError}
              </div>
            )}

            {(uploading || uploadedBytes > 0) && (
              <div className="mt-4">
                <div className="mb-1.5 flex items-center justify-between text-[11px] text-slate-500">
                  <span className="truncate">{uploadLabel || 'Upload complete'}</span>
                  <span className="tabular-nums">
                    {formatBytes(uploadedBytes)} / {formatBytes(uploadTotal)}
                  </span>
                </div>
                <ProgressBar
                  value={uploadTotal ? (uploadedBytes / uploadTotal) * 100 : 0}
                  indeterminate={uploading && uploadedBytes === 0}
                />
              </div>
            )}

            <Button
              className="mt-4 w-full"
              size="lg"
              onClick={startUpload}
              loading={uploading}
              disabled={!file}
            >
              Upload & import
            </Button>
            <p className="mt-3 text-[11px] leading-5 text-slate-400">
              Large files may take several minutes. You can leave this page; processing continues on the engine.
            </p>
          </div>
        </Card>

        <Card>
          <CardHeader
            title="Import history"
            description="Live job progress with per-item quarantine details."
            action={
              <Button size="sm" variant="ghost" onClick={() => jobs.refetch()} loading={jobs.isFetching}>
                <RotateCw className="h-3.5 w-3.5" /> Refresh
              </Button>
            }
          />
          <div className="overflow-x-auto">
            <table className="data-table w-full">
              <thead className="border-b border-slate-100 bg-slate-50/70">
                <tr>
                  <th>File</th>
                  <th>Status</th>
                  <th className="w-[28%]">Progress</th>
                  <th>Results</th>
                  <th>Created</th>
                  <th />
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {jobs.isLoading ? (
                  <tr>
                    <td colSpan={6} className="h-48 text-center">
                      <Spinner className="mx-auto h-5 w-5 text-slate-400" />
                    </td>
                  </tr>
                ) : jobList.length === 0 ? (
                  <tr>
                    <td colSpan={6}>
                      <EmptyState
                        icon={<FileArchive className="h-5 w-5" />}
                        title="No imports yet"
                        description="Upload a PST or mbox file to start building the archive."
                      />
                    </td>
                  </tr>
                ) : (
                  jobList.map((job) => {
                    const done = job.processed_items + job.error_items
                    const pct = job.total_items > 0 ? (done / job.total_items) * 100 : job.status === 'completed' ? 100 : 0
                    return (
                      <tr key={job.id}>
                        <td>
                          <p className="max-w-56 truncate text-xs font-medium text-slate-800">{job.original_filename}</p>
                          <p className="mt-0.5 whitespace-nowrap text-[10px] uppercase tracking-wide text-slate-400">
                            {job.kind} · {formatBytes(job.size_bytes)}
                          </p>
                        </td>
                        <td>
                          <Badge tone={statusTone(job.status)}>{job.status.replace('_', ' ')}</Badge>
                          {job.error_message && (
                            <p className="mt-1 max-w-48 truncate text-[10px] text-rose-600" title={job.error_message}>
                              {job.error_message}
                            </p>
                          )}
                        </td>
                        <td>
                          <ProgressBar
                            value={pct}
                            indeterminate={job.status === 'running' && job.total_items === 0}
                          />
                          <p className="mt-1 text-[10px] tabular-nums text-slate-500">
                            {done.toLocaleString()} / {job.total_items ? job.total_items.toLocaleString() : '?'} items
                          </p>
                        </td>
                        <td>
                          <p className="text-[11px] text-slate-600">{job.processed_items.toLocaleString()} archived</p>
                          <p className="text-[10px] text-slate-400">
                            {job.duplicate_items.toLocaleString()} duplicate · {job.error_items.toLocaleString()} error
                          </p>
                        </td>
                        <td>
                          <p className="text-[11px] text-slate-600">{relativeTime(job.created_at)}</p>
                          <p className="text-[10px] text-slate-400">{formatDateTime(job.created_at)}</p>
                        </td>
                        <td className="text-right">
                          <Button
                            size="sm"
                            variant="ghost"
                            disabled={job.error_items === 0 && job.status !== 'failed'}
                            onClick={() => setErrorJob(job)}
                          >
                            <FileText className="h-3.5 w-3.5" /> Errors
                          </Button>
                        </td>
                      </tr>
                    )
                  })
                )}
              </tbody>
            </table>
          </div>
        </Card>
      </div>

      <Modal
        open={Boolean(errorJob)}
        onClose={() => setErrorJob(null)}
        title="Quarantined items"
        description={errorJob ? `${errorJob.original_filename} · ${errorJob.error_items} item(s) failed` : undefined}
        width="max-w-2xl"
      >
        {errors.isLoading ? (
          <Spinner className="mx-auto my-8 h-5 w-5 text-slate-400" />
        ) : (errors.data?.errors.length ?? 0) === 0 ? (
          <p className="py-6 text-center text-xs text-slate-500">
            No item-level errors recorded. Fatal job errors appear in the job row.
          </p>
        ) : (
          <div className="max-h-[55vh] space-y-2 overflow-y-auto">
            {errors.data?.errors.map((err) => (
              <div key={err.id} className="rounded-lg border border-slate-200 bg-slate-50 p-3">
                <div className="flex items-center justify-between gap-2">
                  <p className="truncate font-mono text-[10px] text-slate-500">{err.item_ref ?? 'item'}</p>
                  <span className="shrink-0 text-[10px] text-slate-400">{formatDateTime(err.created_at)}</span>
                </div>
                <p className="mt-1 break-words text-xs text-rose-700">{err.error}</p>
              </div>
            ))}
          </div>
        )}
      </Modal>
    </div>
  )
}

function Metric({
  label,
  value,
  icon,
  tone,
}: {
  label: string
  value: number
  icon: React.ReactNode
  tone: 'amber' | 'emerald' | 'rose' | 'indigo'
}) {
  const tones = {
    amber: 'bg-amber-50 text-amber-600',
    emerald: 'bg-emerald-50 text-emerald-600',
    rose: 'bg-rose-50 text-rose-600',
    indigo: 'bg-indigo-50 text-indigo-600',
  }
  return (
    <div className="flex items-center gap-3 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <div className={cn('flex h-9 w-9 items-center justify-center rounded-lg', tones[tone])}>{icon}</div>
      <div>
        <p className="text-lg font-semibold tabular-nums tracking-tight text-slate-900">{value.toLocaleString()}</p>
        <p className="text-[11px] text-slate-500">{label}</p>
      </div>
    </div>
  )
}
