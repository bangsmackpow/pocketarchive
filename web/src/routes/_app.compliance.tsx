import * as React from 'react'
import { createFileRoute, useRouteContext } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, ChevronRight, Filter, Lock, RotateCw, Scale, ShieldCheck } from 'lucide-react'
import { apiFetch } from '~/lib/api'
import { formatDate, formatDateTime } from '~/lib/format'
import type { AuditLog, SearchResponse, Tenant } from '~/lib/types'
import {
  Badge,
  Button,
  Card,
  CardHeader,
  EmptyState,
  Input,
  Label,
  Modal,
  Pagination,
  Spinner,
  Tabs,
  Textarea,
} from '~/components/ui'

export const Route = createFileRoute('/_app/compliance')({
  component: CompliancePage,
})

function CompliancePage() {
  const session = useRouteContext({ from: '/_app' })
  const [tab, setTab] = React.useState('retention')

  return (
    <div className="mx-auto max-w-[1400px]">
      <div className="mb-5">
        <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-indigo-600">
          Governance
        </p>
        <h2 className="mt-1 text-xl font-semibold tracking-tight text-slate-900">
          Retention, legal holds & audit trail
        </h2>
        <p className="mt-1 max-w-2xl text-xs leading-5 text-slate-500">
          Retention is enforced at the database layer. Legal holds override
          retention, and every search, view, export and configuration change is
          recorded in an append-only audit log.
        </p>
      </div>

      <Tabs
        active={tab}
        onChange={setTab}
        className="mb-4"
        tabs={[
          { id: 'retention', label: 'Retention policy' },
          { id: 'holds', label: 'Legal holds' },
          { id: 'audit', label: 'Audit log' },
        ]}
      />

      {tab === 'retention' && <RetentionPanel canManage={session.user.role === 'admin'} />}
      {tab === 'holds' && <LegalHoldsPanel canManage={session.user.role === 'admin'} />}
      {tab === 'audit' && <AuditPanel />}
    </div>
  )
}

function RetentionPanel({ canManage }: { canManage: boolean }) {
  const queryClient = useQueryClient()
  const tenant = useQuery({
    queryKey: ['tenant'],
    queryFn: () => apiFetch<Tenant>('/tenant'),
  })
  const [days, setDays] = React.useState('')
  const [saved, setSaved] = React.useState(false)

  React.useEffect(() => {
    if (tenant.data) setDays(String(tenant.data.retention_days))
  }, [tenant.data])

  const save = useMutation({
    mutationFn: () =>
      apiFetch<Tenant>('/tenant/settings', {
        method: 'PUT',
        body: JSON.stringify({ retention_days: Number(days) }),
      }),
    onSuccess: (data) => {
      queryClient.setQueryData(['tenant'], data)
      setSaved(true)
      window.setTimeout(() => setSaved(false), 2000)
    },
  })

  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_380px]">
      <Card>
        <CardHeader
          title="Default retention period"
          description="Applied to newly archived messages. Existing messages keep their immutable retain_until timestamp."
        />
        <div className="p-5">
          <div className="max-w-xs">
            <Label>Retention days</Label>
            <Input
              type="number"
              min={1}
              max={36500}
              value={days}
              disabled={!canManage}
              onChange={(e) => setDays(e.target.value)}
            />
            <p className="mt-2 text-[11px] text-slate-500">
              {Number(days) >= 365 ? `${(Number(days) / 365).toFixed(1)} years` : 'Less than one year'}
            </p>
          </div>
          {canManage ? (
            <div className="mt-5 flex items-center gap-3">
              <Button
                loading={save.isPending}
                disabled={!days || Number(days) < 1 || Number(days) === tenant.data?.retention_days}
                onClick={() => save.mutate()}
              >
                Save retention policy
              </Button>
              {saved && <span className="text-xs font-medium text-emerald-600">Policy updated</span>}
              {save.isError && (
                <span className="text-xs text-rose-600">
                  {save.error instanceof Error ? save.error.message : 'Update failed'}
                </span>
              )}
            </div>
          ) : (
            <p className="mt-4 text-xs text-slate-500">
              Administrator role required to modify retention.
            </p>
          )}
        </div>
      </Card>

      <Card className="h-fit">
        <CardHeader title="How retention works" />
        <ul className="space-y-3 p-5 text-[11px] leading-5 text-slate-500">
          <li className="flex gap-2"><ShieldCheck className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-500" /> The engine stamps every message with an immutable retain_until date at ingestion.</li>
          <li className="flex gap-2"><ShieldCheck className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-500" /> Deletion is rejected before retain_until, even by administrators.</li>
          <li className="flex gap-2"><ShieldCheck className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-500" /> A legal hold blocks deletion regardless of retention.</li>
          <li className="flex gap-2"><ShieldCheck className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-500" /> Retention cannot be shortened after ingestion; it can only be extended.</li>
          <li className="flex gap-2"><ShieldCheck className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-500" /> PostgreSQL triggers enforce the same rules outside the API.</li>
        </ul>
      </Card>
    </div>
  )
}

function LegalHoldsPanel({ canManage }: { canManage: boolean }) {
  const queryClient = useQueryClient()
  const [page, setPage] = React.useState(1)
  const [release, setRelease] = React.useState<{ id: string; subject: string } | null>(null)
  const [reason, setReason] = React.useState('')

  const holds = useQuery({
    queryKey: ['legal-holds', page],
    queryFn: () =>
      apiFetch<SearchResponse>(`/search?legal_hold=true&limit=20&page=${page}&source=journal,pst,mbox`),
  })

  const releaseHold = useMutation({
    mutationFn: (id: string) =>
      apiFetch(`/messages/${id}/legal-hold`, {
        method: 'POST',
        body: JSON.stringify({ hold: false, reason }),
      }),
    onSuccess: async () => {
      setRelease(null)
      setReason('')
      await queryClient.invalidateQueries({ queryKey: ['legal-holds'] })
    },
  })

  return (
    <Card>
      <CardHeader
        title="Active legal holds"
        description="Messages preserved for litigation, investigation or regulatory requests."
        action={
          <Button size="sm" variant="ghost" onClick={() => holds.refetch()} loading={holds.isFetching}>
            <RotateCw className="h-3.5 w-3.5" /> Refresh
          </Button>
        }
      />
      <div className="overflow-x-auto">
        <table className="data-table w-full">
          <thead className="border-b border-slate-100 bg-slate-50/70">
            <tr>
              <th>Subject</th>
              <th>From</th>
              <th>Date</th>
              <th>Source</th>
              <th className="text-right">Action</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {holds.isLoading ? (
              <tr><td colSpan={5} className="h-40 text-center"><Spinner className="mx-auto h-5 w-5 text-slate-400" /></td></tr>
            ) : holds.data?.hits.length ? (
              holds.data.hits.map((hit) => (
                <tr key={hit.id}>
                  <td>
                    <p className="max-w-md truncate text-xs font-medium text-slate-800">{hit.subject || '(no subject)'}</p>
                    <Badge tone="rose" className="mt-1"><Lock className="h-2.5 w-2.5" /> Legal hold</Badge>
                  </td>
                  <td className="text-[11px] text-slate-500">{hit.from_addr}</td>
                  <td className="text-[11px] text-slate-500">{formatDate(hit.sent_at)}</td>
                  <td><Badge tone={hit.source_type === 'journal' ? 'indigo' : 'neutral'}>{hit.source_type}</Badge></td>
                  <td className="text-right">
                    <Button
                      size="sm"
                      variant="secondary"
                      disabled={!canManage}
                      onClick={() => setRelease({ id: hit.id, subject: hit.subject })}
                    >
                      Release
                    </Button>
                  </td>
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={5}>
                  <EmptyState
                    icon={<Scale className="h-5 w-5" />}
                    title="No active legal holds"
                    description="Place a hold from any message preview to preserve it beyond retention."
                  />
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {holds.data && holds.data.total > 0 && (
        <Pagination page={page} limit={20} total={holds.data.total} onPage={setPage} />
      )}

      <Modal
        open={Boolean(release)}
        onClose={() => setRelease(null)}
        title="Release legal hold"
        description={release ? `“${release.subject || '(no subject)'}” will follow normal retention rules again.` : undefined}
        footer={
          <>
            <Button variant="secondary" onClick={() => setRelease(null)}>Cancel</Button>
            <Button
              variant="danger"
              loading={releaseHold.isPending}
              onClick={() => release && releaseHold.mutate(release.id)}
            >
              Release hold
            </Button>
          </>
        }
      >
        <Label>Release note (optional)</Label>
        <Textarea
          rows={3}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder="Matter closed; preservation requirement ended."
        />
      </Modal>
    </Card>
  )
}

function AuditPanel() {
  const [action, setAction] = React.useState('')
  const [actor, setActor] = React.useState('')
  const [dateFrom, setDateFrom] = React.useState('')
  const [dateTo, setDateTo] = React.useState('')
  const [offset, setOffset] = React.useState(0)
  const [expanded, setExpanded] = React.useState<number | null>(null)
  const limit = 50

  const params = new URLSearchParams()
  if (action) params.set('action', action)
  if (actor) params.set('actor', actor)
  if (dateFrom) params.set('date_from', dateFrom)
  if (dateTo) params.set('date_to', dateTo)
  params.set('limit', String(limit))
  params.set('offset', String(offset))

  const logs = useQuery({
    queryKey: ['audit', params.toString()],
    queryFn: () => apiFetch<{ logs: AuditLog[]; limit: number; offset: number }>(`/audit-logs?${params}`),
  })

  function update(filter: () => void) {
    filter()
    setOffset(0)
  }

  return (
    <Card>
      <CardHeader
        title="Audit log"
        description="Append-only record of authentication, ingestion, search, access and compliance events."
        action={<Button size="sm" variant="ghost" onClick={() => logs.refetch()} loading={logs.isFetching}><RotateCw className="h-3.5 w-3.5" /> Refresh</Button>}
      />
      <div className="grid gap-3 border-b border-slate-100 bg-slate-50/50 p-4 sm:grid-cols-2 lg:grid-cols-4">
        <div>
          <Label>Action</Label>
          <Input value={action} onChange={(e) => update(() => setAction(e.target.value))} placeholder="e.g. message.view" />
        </div>
        <div>
          <Label>Actor</Label>
          <Input value={actor} onChange={(e) => update(() => setActor(e.target.value))} placeholder="email contains…" />
        </div>
        <div>
          <Label>From</Label>
          <Input type="date" value={dateFrom} onChange={(e) => update(() => setDateFrom(e.target.value))} />
        </div>
        <div>
          <Label>To</Label>
          <Input type="date" value={dateTo} onChange={(e) => update(() => setDateTo(e.target.value))} />
        </div>
      </div>

      <div className="overflow-x-auto">
        <table className="data-table w-full">
          <thead className="border-b border-slate-100 bg-slate-50/70">
            <tr>
              <th>Time</th>
              <th>Action</th>
              <th>Actor</th>
              <th>Target</th>
              <th>IP</th>
              <th />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {logs.isLoading ? (
              <tr><td colSpan={6} className="h-40 text-center"><Spinner className="mx-auto h-5 w-5 text-slate-400" /></td></tr>
            ) : logs.data?.logs.length ? (
              logs.data.logs.map((log) => (
                <React.Fragment key={log.id}>
                  <tr className="cursor-pointer hover:bg-slate-50" onClick={() => setExpanded(expanded === log.id ? null : log.id)}>
                    <td className="whitespace-nowrap text-[11px] text-slate-500">{formatDateTime(log.created_at)}</td>
                    <td>
                      <span className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-[10px] text-slate-700">{log.action}</span>
                    </td>
                    <td className="text-[11px] text-slate-600">{log.actor_email || 'system'}</td>
                    <td className="text-[11px] text-slate-500">
                      {log.target_type ? `${log.target_type}:${log.target_id ?? ''}` : '—'}
                    </td>
                    <td className="text-[11px] text-slate-500">{log.ip || '—'}</td>
                    <td className="text-right">
                      {expanded === log.id ? <ChevronDown className="ml-auto h-3.5 w-3.5 text-slate-400" /> : <ChevronRight className="ml-auto h-3.5 w-3.5 text-slate-400" />}
                    </td>
                  </tr>
                  {expanded === log.id && (
                    <tr>
                      <td colSpan={6} className="bg-slate-50">
                        <pre className="overflow-x-auto rounded-lg border border-slate-200 bg-white p-3 font-mono text-[10px] leading-5 text-slate-600">
{JSON.stringify(log.detail, null, 2)}
                        </pre>
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              ))
            ) : (
              <tr>
                <td colSpan={6}>
                  <EmptyState icon={<Filter className="h-5 w-5" />} title="No audit entries match those filters" />
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <div className="flex items-center justify-between border-t border-slate-100 px-4 py-3 text-xs text-slate-500">
        <span>{logs.data?.logs.length ?? 0} entries</span>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="secondary" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - limit))}>Previous</Button>
          <Button
            size="sm"
            variant="secondary"
            disabled={(logs.data?.logs.length ?? 0) < limit}
            onClick={() => setOffset(offset + limit)}
          >
            Next
          </Button>
        </div>
      </div>
    </Card>
  )
}

