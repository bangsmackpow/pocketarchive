import * as React from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import {
  ArrowUpRight,
  Check,
  Clipboard,
  Clock,
  Inbox,
  Lock,
  Mail,
  Server,
  ShieldCheck,
  TriangleAlert,
} from 'lucide-react'
import { apiFetch } from '~/lib/api'
import { formatBytes, relativeTime } from '~/lib/format'
import type { IngestionSource, SetupInfo, Stats } from '~/lib/types'
import { Badge, Button, Card, CardHeader, EmptyState, Spinner } from '~/components/ui'

export const Route = createFileRoute('/_app/sources')({
  component: SourcesPage,
})

function SourcesPage() {
  const setup = useQuery({
    queryKey: ['setup'],
    queryFn: () => apiFetch<SetupInfo>('/setup'),
  })
  const stats = useQuery({
    queryKey: ['stats'],
    queryFn: () => apiFetch<Stats>('/stats'),
    refetchInterval: 10_000,
  })
  const sources = useQuery({
    queryKey: ['sources'],
    queryFn: () => apiFetch<{ sources: IngestionSource[] }>('/sources'),
  })

  const smtp = setup.data?.smtp
  const endpoint = smtp ? `${smtp.hostname}:${smtp.port}` : '—'

  return (
    <div className="mx-auto max-w-[1400px]">
      <div className="mb-5">
        <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-indigo-600">
          Journaling
        </p>
        <h2 className="mt-1 text-xl font-semibold tracking-tight text-slate-900">
          Office 365 ingestion source
        </h2>
        <p className="mt-1 max-w-2xl text-xs leading-5 text-slate-500">
          Microsoft 365 journaling forwards a copy of every inbound and outbound
          message to this receiver. Messages are spooled to disk, acknowledged
          with 250 OK, then encrypted asynchronously.
        </p>
      </div>

      {setup.isLoading ? (
        <Spinner className="my-16 mx-auto h-6 w-6 text-slate-400" />
      ) : setup.isError || !setup.data ? (
        <Card>
          <EmptyState
            icon={<TriangleAlert className="h-5 w-5" />}
            title="Could not load SMTP setup"
            description="The engine did not return its journaling configuration."
            action={<Button onClick={() => setup.refetch()}>Retry</Button>}
          />
        </Card>
      ) : (
        <>
          <div className="grid gap-4 lg:grid-cols-3">
            <Card className="lg:col-span-2">
              <CardHeader
                title="SMTP journal endpoint"
                description="Point your Exchange journaling rule at this host and port. STARTTLS is advertised automatically."
                action={<Badge tone="emerald"><ShieldCheck className="h-3 w-3" /> Ready</Badge>}
              />
              <div className="p-5">
                <CopyField label="Host and port" value={endpoint} mono />
                <div className="mt-4 grid gap-4 sm:grid-cols-2">
                  <InfoBlock label="Transport" value={setup.data.journaling.transport} icon={<Lock className="h-3.5 w-3.5" />} />
                  <InfoBlock label="TLS mode" value={smtp?.tls_mode.replaceAll('-', ' ') ?? '—'} icon={<ShieldCheck className="h-3.5 w-3.5" />} />
                  <InfoBlock
                    label="Allowed sender domains"
                    value={smtp?.allowed_domains.length ? smtp.allowed_domains.join(', ') : 'None configured — mail will be rejected'}
                    icon={<Mail className="h-3.5 w-3.5" />}
                  />
                  <InfoBlock
                    label="SMTP AUTH"
                    value={smtp?.auth_enabled ? 'Enabled (PLAIN over TLS)' : 'Not required'}
                    icon={<Lock className="h-3.5 w-3.5" />}
                  />
                </div>
                {smtp?.tls_mode === 'automatic-self-signed' && (
                  <div className="mt-4 flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 text-[11px] leading-5 text-amber-800">
                    <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                    A self-signed certificate is active. Microsoft 365 requires a
                    publicly trusted certificate for journaling; mount your TLS
                    certificate into the engine and set SMTP_TLS_CERT_FILE /
                    SMTP_TLS_KEY_FILE.
                  </div>
                )}
              </div>
            </Card>

            <Card>
              <CardHeader title="Journal metrics" description="Last 24 hours" />
              <div className="grid grid-cols-2 gap-px bg-slate-100">
                <Metric label="Journal messages" value={(stats.data?.journal_messages_24h ?? 0).toLocaleString()} icon={<Inbox className="h-4 w-4" />} />
                <Metric label="All messages" value={(stats.data?.messages_24h ?? 0).toLocaleString()} icon={<Mail className="h-4 w-4" />} />
                <Metric label="Last received" value={relativeTime(stats.data?.last_journal_at)} icon={<Clock className="h-4 w-4" />} />
                <Metric label="Queue backlog" value={(stats.data?.queue_backlog ?? 0).toLocaleString()} icon={<Server className="h-4 w-4" />} />
                <Metric label="Queue failures" value={(stats.data?.queue_failed ?? 0).toLocaleString()} icon={<TriangleAlert className="h-4 w-4" />} />
                <Metric label="Blob storage" value={formatBytes(stats.data?.storage_bytes ?? 0)} icon={<Lock className="h-4 w-4" />} />
              </div>
            </Card>
          </div>

          <div className="mt-4 grid gap-4 lg:grid-cols-[minmax(0,1fr)_390px]">
            <Card>
              <CardHeader
                title="Connect Microsoft 365 journaling"
                description="Exchange Online requires an outbound connector or a journaling rule that delivers over SMTP."
              />
              <ol className="divide-y divide-slate-100">
                {[
                  {
                    title: 'Create the journal mailbox',
                    body: `Create a shared mailbox or mail contact for ${setup.data.journaling.address}. This is the journaling target, not a user mailbox.`,
                  },
                  {
                    title: 'Add a journal rule',
                    body: 'In the Exchange admin center, open Compliance management → Journal rules → New rule. Journal all messages (internal and external) and send reports to the journaling address.',
                  },
                  {
                    title: 'Allow the sender domain',
                    body: `Add your Microsoft 365 domain to SMTP_ALLOWED_DOMAINS (currently: ${smtp?.allowed_domains.join(', ') || 'none'}). Optionally restrict SMTP_ALLOWED_IPS if Microsoft publishes stable ranges for your tenant.`,
                  },
                  {
                    title: 'Publish a trusted TLS certificate',
                    body: `Mount a certificate for ${smtp?.hostname} and restart the engine. Set SMTP_TLS_CERT_FILE and SMTP_TLS_KEY_FILE, then expose port ${smtp?.port} through your firewall.`,
                  },
                  {
                    title: 'Send a test message',
                    body: 'Send an inbound and outbound email, then return here. Journal messages should appear in Archive search within seconds with source "journal".',
                  },
                ].map((step, index) => (
                  <li key={step.title} className="flex gap-4 px-5 py-4">
                    <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-indigo-50 text-[11px] font-semibold text-indigo-700">
                      {index + 1}
                    </span>
                    <div>
                      <p className="text-xs font-medium text-slate-800">{step.title}</p>
                      <p className="mt-1 text-[11px] leading-5 text-slate-500">{step.body}</p>
                    </div>
                  </li>
                ))}
              </ol>
            </Card>

            <div className="space-y-4">
              <Card>
                <CardHeader title="Ingestion sources" description="Configured pipelines for this tenant" />
                <div className="p-4">
                  {sources.isLoading ? (
                    <Spinner className="mx-auto my-6 h-4 w-4 text-slate-400" />
                  ) : sources.data?.sources.length ? (
                    <ul className="space-y-2">
                      {sources.data.sources.map((source) => (
                        <li key={source.id} className="flex items-center justify-between gap-3 rounded-lg border border-slate-200 px-3 py-2.5">
                          <div>
                            <p className="text-xs font-medium text-slate-800">{source.name}</p>
                            <p className="mt-0.5 text-[10px] uppercase tracking-wide text-slate-400">
                              {source.kind} · {source.config.provider === 'microsoft365' ? 'Microsoft 365' : 'manual'}
                            </p>
                          </div>
                          <Badge tone={source.enabled ? 'emerald' : 'neutral'}>
                            {source.enabled ? 'Enabled' : 'Disabled'}
                          </Badge>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p className="py-6 text-center text-xs text-slate-500">No ingestion sources configured.</p>
                  )}
                </div>
              </Card>

              <Card>
                <div className="p-5">
                  <div className="flex items-center gap-2 text-xs font-semibold text-slate-800">
                    <ShieldCheck className="h-4 w-4 text-indigo-500" />
                    Security posture
                  </div>
                  <ul className="mt-3 space-y-2.5 text-[11px] leading-5 text-slate-500">
                    <li className="flex gap-2"><Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-500" /> TLS 1.2+ required for SMTP AUTH</li>
                    <li className="flex gap-2"><Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-500" /> Sender domain/IP allowlist enforced</li>
                    <li className="flex gap-2"><Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-500" /> Raw messages spooled before 250 OK</li>
                    <li className="flex gap-2"><Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-500" /> Journal envelope preserved for provenance</li>
                    <li className="flex gap-2"><Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-500" /> Per-tenant AES-256-GCM blob keys</li>
                  </ul>
                  <a
                    href="https://learn.microsoft.com/en-us/purview/ediscovery-journaling"
                    target="_blank"
                    rel="noreferrer"
                    className="mt-4 inline-flex items-center gap-1 text-[11px] font-medium text-indigo-600 hover:text-indigo-500"
                  >
                    Microsoft journaling documentation <ArrowUpRight className="h-3 w-3" />
                  </a>
                </div>
              </Card>
            </div>
          </div>
        </>
      )}
    </div>
  )
}

function CopyField({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  const [copied, setCopied] = React.useState(false)
  return (
    <div>
      <p className="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-slate-400">{label}</p>
      <div className="flex items-center gap-2 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2.5">
        <code className={mono ? 'flex-1 font-mono text-sm text-slate-800' : 'flex-1 text-sm text-slate-800'}>{value}</code>
        <Button
          size="sm"
          variant="secondary"
          onClick={async () => {
            await navigator.clipboard.writeText(value)
            setCopied(true)
            window.setTimeout(() => setCopied(false), 1500)
          }}
        >
          {copied ? <Check className="h-3.5 w-3.5 text-emerald-600" /> : <Clipboard className="h-3.5 w-3.5" />}
          {copied ? 'Copied' : 'Copy'}
        </Button>
      </div>
    </div>
  )
}

function InfoBlock({ label, value, icon }: { label: string; value: string; icon: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-slate-200 px-3 py-2.5">
      <div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-slate-400">
        {icon}
        {label}
      </div>
      <p className="mt-1 break-words text-xs text-slate-700">{value}</p>
    </div>
  )
}

function Metric({ label, value, icon }: { label: string; value: string; icon: React.ReactNode }) {
  return (
    <div className="bg-white p-4">
      <div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-slate-400">
        {icon}
        {label}
      </div>
      <p className="mt-1.5 text-sm font-semibold tabular-nums text-slate-900">{value}</p>
    </div>
  )
}

