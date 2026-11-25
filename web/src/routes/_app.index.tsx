import * as React from 'react'
import { createFileRoute, useRouteContext } from '@tanstack/react-router'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  CalendarDays,
  Download,
  FileText,
  Filter,
  Inbox,
  Loader2,
  Lock,
  Paperclip,
  RotateCw,
  Scale,
  Search,
  SlidersHorizontal,
  X,
} from 'lucide-react'
import { apiFetch, downloadUrl } from '~/lib/api'
import { cn, formatBytes, formatDate, formatDateTime, formatTime } from '~/lib/format'
import type { MessageDetail, SearchHit, SearchResponse } from '~/lib/types'
import {
  Badge,
  Button,
  EmptyState,
  Input,
  Label,
  Modal,
  Pagination,
  Select,
  Spinner,
  Tabs,
  Textarea,
} from '~/components/ui'

export const Route = createFileRoute('/_app/')({
  component: SearchPage,
})

interface Filters {
  from: string
  to: string
  dateFrom: string
  dateTo: string
  hasAttachment: '' | 'true' | 'false'
  sources: string[]
  legalHold: '' | 'true' | 'false'
}

const emptyFilters: Filters = {
  from: '',
  to: '',
  dateFrom: '',
  dateTo: '',
  hasAttachment: '',
  sources: [],
  legalHold: '',
}

function activeFilterCount(f: Filters): number {
  return [
    f.from,
    f.to,
    f.dateFrom,
    f.dateTo,
    f.hasAttachment,
    f.legalHold,
    f.sources.length ? 'sources' : '',
  ].filter(Boolean).length
}

function SearchPage() {
  const session = useRouteContext({ from: '/_app' })
  const [qInput, setQInput] = React.useState('')
  const [q, setQ] = React.useState('')
  const [filters, setFilters] = React.useState<Filters>(emptyFilters)
  const [page, setPage] = React.useState(1)
  const [selected, setSelected] = React.useState<string | null>(null)
  const [showFilters, setShowFilters] = React.useState(false)
  const limit = 25

  React.useEffect(() => {
    const timer = window.setTimeout(() => {
      setQ(qInput.trim())
      setPage(1)
    }, 280)
    return () => window.clearTimeout(timer)
  }, [qInput])

  const params = React.useMemo(() => {
    const sp = new URLSearchParams()
    if (q) sp.set('q', q)
    if (filters.from) sp.set('from', filters.from)
    if (filters.to) sp.set('to', filters.to)
    if (filters.dateFrom) sp.set('date_from', filters.dateFrom)
    if (filters.dateTo) sp.set('date_to', filters.dateTo)
    if (filters.hasAttachment) sp.set('has_attachment', filters.hasAttachment)
    if (filters.legalHold) sp.set('legal_hold', filters.legalHold)
    if (filters.sources.length) sp.set('source', filters.sources.join(','))
    sp.set('page', String(page))
    sp.set('limit', String(limit))
    return sp.toString()
  }, [q, filters, page])

  const search = useQuery({
    queryKey: ['search', params],
    queryFn: () => apiFetch<SearchResponse>(`/search?${params}`),
    placeholderData: keepPreviousData,
  })

  const hasFilters = activeFilterCount(filters) > 0

  function updateFilters(next: Partial<Filters>) {
    setFilters((f) => ({ ...f, ...next }))
    setPage(1)
  }

  function toggleSource(source: string) {
    setFilters((f) => ({
      ...f,
      sources: f.sources.includes(source)
        ? f.sources.filter((s) => s !== source)
        : [...f.sources, source],
    }))
    setPage(1)
  }

  return (
    <div className="mx-auto max-w-[1600px]">
      <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-indigo-600">
            Message archive
          </p>
          <h2 className="mt-1 text-xl font-semibold tracking-tight text-slate-900">
            Find any message, fast
          </h2>
          <p className="mt-1 text-xs text-slate-500">
            Full-text search across journal, PST and mbox imports. Every result is tenant-scoped.
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs text-slate-500">
          <Badge tone="emerald">
            <Lock className="h-3 w-3" /> Encrypted at rest
          </Badge>
          {search.data && (
            <span className="tabular-nums">
              {search.data.total.toLocaleString()} results · {search.data.took_ms} ms
            </span>
          )}
        </div>
      </div>

      <div className="relative mb-4">
        <Search className="pointer-events-none absolute left-4 top-1/2 h-5 w-5 -translate-y-1/2 text-slate-400" />
        <input
          value={qInput}
          onChange={(e) => setQInput(e.target.value)}
          placeholder="Search subjects, bodies, senders, recipients and attachment names…"
          className="focus-ring h-13 w-full rounded-xl border border-slate-200 bg-white pl-12 pr-28 text-sm text-slate-900 shadow-sm placeholder:text-slate-400 focus:border-indigo-400"
        />
        <div className="absolute right-2 top-1/2 flex -translate-y-1/2 items-center gap-1">
          {search.isFetching && <Loader2 className="mr-2 h-4 w-4 animate-spin text-slate-400" />}
          <Button
            size="sm"
            variant="secondary"
            className="lg:hidden"
            onClick={() => setShowFilters((v) => !v)}
          >
            <SlidersHorizontal className="h-3.5 w-3.5" />
            Filters
            {hasFilters && <span className="rounded bg-indigo-100 px-1 text-[10px] text-indigo-700">{activeFilterCount(filters)}</span>}
          </Button>
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-[260px_minmax(0,1fr)]">
        <aside className={cn('lg:block', showFilters ? 'block' : 'hidden')}>
          <div className="rounded-xl border border-slate-200 bg-white shadow-sm">
            <div className="flex items-center justify-between border-b border-slate-100 px-4 py-3">
              <div className="flex items-center gap-2 text-xs font-semibold text-slate-800">
                <Filter className="h-3.5 w-3.5 text-slate-500" />
                Filters
                {hasFilters && (
                  <span className="rounded-full bg-indigo-50 px-1.5 py-0.5 text-[10px] text-indigo-700">
                    {activeFilterCount(filters)}
                  </span>
                )}
              </div>
              <button
                onClick={() => {
                  setFilters(emptyFilters)
                  setPage(1)
                }}
                className="text-[11px] text-slate-500 hover:text-indigo-600 disabled:opacity-40"
                disabled={!hasFilters}
              >
                Clear
              </button>
            </div>
            <div className="space-y-4 p-4">
              <FilterGroup label="Sender">
                <Input
                  value={filters.from}
                  onChange={(e) => updateFilters({ from: e.target.value })}
                  placeholder="exact@address.com"
                  inputMode="email"
                />
              </FilterGroup>
              <FilterGroup label="Recipient">
                <Input
                  value={filters.to}
                  onChange={(e) => updateFilters({ to: e.target.value })}
                  placeholder="exact@address.com"
                  inputMode="email"
                />
              </FilterGroup>
              <FilterGroup label="Date range">
                <div className="grid grid-cols-2 gap-2">
                  <Input
                    type="date"
                    value={filters.dateFrom}
                    onChange={(e) => updateFilters({ dateFrom: e.target.value })}
                    aria-label="From date"
                  />
                  <Input
                    type="date"
                    value={filters.dateTo}
                    onChange={(e) => updateFilters({ dateTo: e.target.value })}
                    aria-label="To date"
                  />
                </div>
              </FilterGroup>
              <FilterGroup label="Attachments">
                <Select
                  value={filters.hasAttachment}
                  onChange={(e) =>
                    updateFilters({ hasAttachment: e.target.value as Filters['hasAttachment'] })
                  }
                >
                  <option value="">Any message</option>
                  <option value="true">With attachments</option>
                  <option value="false">Without attachments</option>
                </Select>
              </FilterGroup>
              <FilterGroup label="Legal hold">
                <Select
                  value={filters.legalHold}
                  onChange={(e) =>
                    updateFilters({ legalHold: e.target.value as Filters['legalHold'] })
                  }
                >
                  <option value="">Any status</option>
                  <option value="true">On legal hold</option>
                  <option value="false">Not on hold</option>
                </Select>
              </FilterGroup>
              <FilterGroup label="Source">
                <div className="space-y-2">
                  {[
                    ['journal', 'Office 365 journal'],
                    ['pst', 'PST import'],
                    ['mbox', 'mbox import'],
                  ].map(([value, label]) => (
                    <label key={value} className="flex items-center gap-2 text-xs text-slate-600">
                      <input
                        type="checkbox"
                        checked={filters.sources.includes(value)}
                        onChange={() => toggleSource(value)}
                        className="h-3.5 w-3.5 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                      />
                      {label}
                    </label>
                  ))}
                </div>
              </FilterGroup>
            </div>
          </div>
        </aside>

        <section className="min-w-0">
          <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
            <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 px-4 py-3">
              <div className="flex items-center gap-2 text-xs text-slate-500">
                <CalendarDays className="h-3.5 w-3.5" />
                {q ? (
                  <span>
                    Results for <span className="font-medium text-slate-800">“{q}”</span>
                  </span>
                ) : (
                  <span>All archived messages</span>
                )}
              </div>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => search.refetch()}
                loading={search.isFetching}
              >
                <RotateCw className="h-3.5 w-3.5" /> Refresh
              </Button>
            </div>

            {search.isError && (
              <div className="flex items-start gap-2 border-b border-rose-100 bg-rose-50 px-4 py-3 text-xs text-rose-700">
                <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                {search.error instanceof Error ? search.error.message : 'Search failed'}
              </div>
            )}

            <div className="overflow-x-auto">
              <table className="data-table w-full table-fixed">
                <colgroup>
                  <col className="w-[20%]" />
                  <col className="w-[38%]" />
                  <col className="w-[18%]" />
                  <col className="w-[14%]" />
                  <col className="w-[10%]" />
                </colgroup>
                <thead className="border-b border-slate-100 bg-slate-50/70">
                  <tr>
                    <th>From</th>
                    <th>Subject</th>
                    <th>Recipients</th>
                    <th>Date</th>
                    <th className="text-right">Size</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {search.isLoading ? (
                    <tr>
                      <td colSpan={5} className="h-64 text-center text-slate-400">
                        <Spinner className="mx-auto h-5 w-5" />
                      </td>
                    </tr>
                  ) : search.data?.hits.length ? (
                    search.data.hits.map((hit) => (
                      <ResultRow key={hit.id} hit={hit} onOpen={() => setSelected(hit.id)} />
                    ))
                  ) : (
                    <tr>
                      <td colSpan={5}>
                        <EmptyState
                          icon={<Inbox className="h-5 w-5" />}
                          title={hasFilters || q ? 'No messages match those filters' : 'The archive is empty'}
                          description={
                            hasFilters || q
                              ? 'Try removing a filter or using broader search terms.'
                              : 'Configure Office 365 journaling or upload a PST/mbox file to begin.'
                          }
                        />
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>

            {search.data && search.data.total > 0 && (
              <Pagination
                page={page}
                limit={limit}
                total={search.data.total}
                onPage={(p) => {
                  setPage(p)
                  window.scrollTo({ top: 0, behavior: 'smooth' })
                }}
              />
            )}
          </div>
        </section>
      </div>

      {selected && (
        <MessageDrawer
          id={selected}
          onClose={() => setSelected(null)}
          canManage={session.user.role === 'admin'}
          onChanged={() => search.refetch()}
        />
      )}
    </div>
  )
}

function FilterGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <Label>{label}</Label>
      {children}
    </div>
  )
}

function ResultRow({ hit, onOpen }: { hit: SearchHit; onOpen: () => void }) {
  return (
    <tr className="cursor-pointer transition hover:bg-indigo-50/40" onClick={onOpen}>
      <td>
        <p className="truncate text-xs font-medium text-slate-800">{hit.from_name || hit.from_addr || 'Unknown sender'}</p>
        <p className="mt-0.5 truncate text-[11px] text-slate-500">{hit.from_addr}</p>
      </td>
      <td>
        <div className="flex items-center gap-1.5">
          <p className="truncate text-sm font-medium text-slate-900">{hit.subject || '(no subject)'}</p>
          {hit.has_attachments && <Paperclip className="h-3 w-3 shrink-0 text-slate-400" />}
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-1.5">
          <Badge tone={hit.source_type === 'journal' ? 'indigo' : 'neutral'}>
            {hit.source_type}
          </Badge>
          {hit.legal_hold && (
            <Badge tone="rose">
              <Lock className="h-2.5 w-2.5" /> Legal hold
            </Badge>
          )}
          {hit.attachment_count > 0 && (
            <span className="text-[10px] text-slate-400">
              {hit.attachment_count} attachment{hit.attachment_count === 1 ? '' : 's'}
            </span>
          )}
        </div>
      </td>
      <td>
        <p className="truncate text-[11px] text-slate-500">{hit.to_addrs.join(', ') || '—'}</p>
      </td>
      <td>
        <p className="text-xs text-slate-600">{formatDate(hit.sent_at)}</p>
        <p className="text-[10px] text-slate-400">{formatTime(hit.sent_at)}</p>
      </td>
      <td className="text-right text-xs tabular-nums text-slate-500">{formatBytes(hit.size_bytes)}</td>
    </tr>
  )
}

function MessageDrawer({
  id,
  onClose,
  canManage,
  onChanged,
}: {
  id: string
  onClose: () => void
  canManage: boolean
  onChanged: () => void
}) {
  const queryClient = useQueryClient()
  const [tab, setTab] = React.useState('preview')
  const [holdOpen, setHoldOpen] = React.useState(false)
  const [reason, setReason] = React.useState('')

  const detail = useQuery({
    queryKey: ['message', id],
    queryFn: () => apiFetch<MessageDetail>(`/messages/${id}`),
  })

  const hold = useMutation({
    mutationFn: (body: { hold: boolean; reason: string }) =>
      apiFetch(`/messages/${id}/legal-hold`, {
        method: 'POST',
        body: JSON.stringify(body),
      }),
    onSuccess: async () => {
      setHoldOpen(false)
      setReason('')
      await queryClient.invalidateQueries({ queryKey: ['message', id] })
      onChanged()
    },
  })

  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const msg = detail.data

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-950/30 backdrop-blur-[1px]">
      <button className="flex-1 cursor-default" aria-label="Close preview" onClick={onClose} />
      <div className="flex h-full w-full max-w-3xl flex-col border-l border-slate-200 bg-white shadow-2xl">
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-5 py-4">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-1.5">
              <Badge tone={msg?.source_type === 'journal' ? 'indigo' : 'neutral'}>
                {msg?.source_type ?? 'message'}
              </Badge>
              {msg?.legal_hold && (
                <Badge tone="rose">
                  <Lock className="h-2.5 w-2.5" /> On legal hold
                </Badge>
              )}
              {msg?.has_attachments && (
                <Badge tone="amber">
                  <Paperclip className="h-2.5 w-2.5" /> {msg.attachment_count}
                </Badge>
              )}
            </div>
            <h2 className="mt-2 truncate text-base font-semibold tracking-tight text-slate-900">
              {msg?.subject || (detail.isLoading ? 'Loading message…' : '(no subject)')}
            </h2>
            {msg && (
              <p className="mt-1 truncate text-xs text-slate-500">
                {msg.from_name ? `${msg.from_name} <${msg.from_addr}>` : msg.from_addr} · {formatDateTime(msg.sent_at)}
              </p>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {msg && (
              <>
                <a href={downloadUrl(`/messages/${msg.id}/export`)}>
                  <Button size="sm" variant="secondary">
                    <Download className="h-3.5 w-3.5" /> Export .eml
                  </Button>
                </a>
                {canManage && (
                  <Button
                    size="sm"
                    variant={msg.legal_hold ? 'danger' : 'secondary'}
                    onClick={() => {
                      setReason(msg.legal_hold_reason ?? '')
                      setHoldOpen(true)
                    }}
                  >
                    <Scale className="h-3.5 w-3.5" />
                    {msg.legal_hold ? 'Release' : 'Legal hold'}
                  </Button>
                )}
              </>
            )}
            <button
              onClick={onClose}
              className="focus-ring rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700"
              aria-label="Close preview"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        {detail.isError ? (
          <div className="m-5 rounded-lg border border-rose-200 bg-rose-50 p-4 text-xs text-rose-700">
            {detail.error instanceof Error ? detail.error.message : 'Could not load message.'}
          </div>
        ) : !msg ? (
          <div className="flex flex-1 items-center justify-center">
            <Spinner className="h-6 w-6 text-slate-400" />
          </div>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-x-6 gap-y-2 border-b border-slate-100 px-5 py-3 text-xs sm:grid-cols-4">
              <Meta label="From" value={msg.from_addr || '—'} />
              <Meta label="To" value={msg.to_addrs.join(', ') || '—'} />
              <Meta label="Sent" value={formatDateTime(msg.sent_at)} />
              <Meta label="Retained until" value={formatDate(msg.retain_until)} />
            </div>

            <Tabs
              active={tab}
              onChange={setTab}
              className="px-5 pt-2"
              tabs={[
                { id: 'preview', label: 'Preview' },
                { id: 'text', label: 'Plain text' },
                { id: 'attachments', label: `Attachments (${msg.attachments.length})` },
                { id: 'headers', label: 'Headers' },
              ]}
            />

            <div className="min-h-0 flex-1 overflow-y-auto bg-slate-50/60 p-5">
              {tab === 'preview' && (
                <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
                  {msg.body_html ? (
                    <iframe
                      title="Email preview"
                      sandbox=""
                      srcDoc={wrapEmailHTML(msg.body_html)}
                      className="h-[62vh] w-full"
                    />
                  ) : (
                    <pre className="max-h-[62vh] overflow-auto whitespace-pre-wrap break-words p-5 font-sans text-sm leading-6 text-slate-700">
                      {msg.body_text || 'This message has no renderable body.'}
                    </pre>
                  )}
                </div>
              )}

              {tab === 'text' && (
                <pre className="whitespace-pre-wrap break-words rounded-xl border border-slate-200 bg-white p-5 font-sans text-sm leading-6 text-slate-700 shadow-sm">
                  {msg.body_text || 'No plain-text body.'}
                </pre>
              )}

              {tab === 'attachments' && (
                <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
                  {msg.attachments.length === 0 ? (
                    <EmptyState icon={<Paperclip className="h-5 w-5" />} title="No attachments" />
                  ) : (
                    <ul className="divide-y divide-slate-100">
                      {msg.attachments.map((att) => (
                        <li key={att.id} className="flex items-center gap-3 px-4 py-3">
                          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-slate-100 text-slate-500">
                            <FileText className="h-4 w-4" />
                          </div>
                          <div className="min-w-0 flex-1">
                            <p className="truncate text-xs font-medium text-slate-800">{att.filename}</p>
                            <p className="text-[11px] text-slate-500">
                              {att.content_type} · {formatBytes(att.size_bytes)}
                            </p>
                          </div>
                          <a href={downloadUrl(`/messages/${msg.id}/attachments/${att.id}`)}>
                            <Button size="sm" variant="secondary">
                              <Download className="h-3.5 w-3.5" /> Download
                            </Button>
                          </a>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              )}

              {tab === 'headers' && (
                <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
                  <table className="w-full text-xs">
                    <tbody className="divide-y divide-slate-100">
                      {Object.entries(msg.headers)
                        .sort(([a], [b]) => a.localeCompare(b))
                        .map(([key, values]) => (
                          <tr key={key}>
                            <td className="w-48 bg-slate-50/70 px-4 py-2 align-top font-mono text-[11px] text-slate-500">{key}</td>
                            <td className="break-all px-4 py-2 font-mono text-[11px] text-slate-700">{values.join('\n')}</td>
                          </tr>
                        ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>

            <div className="flex flex-wrap items-center justify-between gap-2 border-t border-slate-100 px-5 py-2.5 text-[10px] text-slate-400">
              <span className="truncate font-mono">SHA-256 {msg.raw_sha256.slice(0, 24)}…</span>
              <span>{formatBytes(msg.size_bytes)} · {msg.attachments.length} attachment(s)</span>
            </div>
          </>
        )}

        <Modal
          open={holdOpen}
          onClose={() => setHoldOpen(false)}
          title={msg?.legal_hold ? 'Release legal hold' : 'Place on legal hold'}
          description={
            msg?.legal_hold
              ? 'The message remains archived and immutable. Retention rules will apply again after release.'
              : 'Legal hold prevents deletion regardless of retention expiry. A reason is recorded in the audit log.'
          }
          footer={
            <>
              <Button variant="secondary" onClick={() => setHoldOpen(false)}>
                Cancel
              </Button>
              <Button
                variant={msg?.legal_hold ? 'danger' : 'primary'}
                loading={hold.isPending}
                disabled={!msg?.legal_hold && reason.trim().length === 0}
                onClick={() =>
                  hold.mutate({
                    hold: !msg?.legal_hold,
                    reason: reason.trim(),
                  })
                }
              >
                {msg?.legal_hold ? 'Release hold' : 'Apply hold'}
              </Button>
            </>
          }
        >
          {!msg?.legal_hold && (
            <div>
              <Label>Reason</Label>
              <Textarea
                rows={3}
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder="e.g. Litigation matter 2026-014, preserve all correspondence with…"
              />
            </div>
          )}
          {hold.isError && (
            <p className="mt-3 text-xs text-rose-600">
              {hold.error instanceof Error ? hold.error.message : 'Legal hold update failed.'}
            </p>
          )}
        </Modal>
      </div>
    </div>
  )
}

function Meta({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <p className="text-[10px] font-semibold uppercase tracking-wider text-slate-400">{label}</p>
      <p className="mt-0.5 truncate text-slate-700" title={value}>
        {value}
      </p>
    </div>
  )
}

function wrapEmailHTML(body: string): string {
  return `<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:; media-src data:;" />
<meta name="referrer" content="no-referrer" />
<style>
  html, body { margin: 0; padding: 16px; background: #fff; color: #1e293b; font: 14px/1.55 ui-sans-serif, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; }
  img { max-width: 100%; height: auto; }
  table { max-width: 100%; }
  a { color: #4f46e5; }
  blockquote { border-left: 3px solid #e2e8f0; margin-left: 0; padding-left: 12px; color: #64748b; }
</style>
</head>
<body>${body}</body>
</html>`
}

