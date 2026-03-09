import { NextResponse } from 'next/server'
import { getSiteConfig } from '@/lib/config'
import { getRecentIncidents, getActiveMaintenances } from '@/lib/incidents'
import { checkAllServices } from '@/lib/health-checker'
import { calculateOverallStatus } from '@/lib/types'
import type { Incident, ScheduledMaintenance } from '@/lib/types'

function escapeXml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;')
}

function incidentToEntry(incident: Incident, siteUrl: string): string {
  const updatedAt = incident.updates.length > 0
    ? incident.updates[incident.updates.length - 1].createdAt
    : incident.createdAt

  const updatesHtml = incident.updates
    .map(u => `<p><strong>${escapeXml(u.status ?? 'update')}</strong>: ${escapeXml(u.message)}</p>`)
    .join('\n')

  const content = `
    <p>Severity: ${escapeXml(incident.severity)} | Status: ${escapeXml(incident.status)}</p>
    <p>Affected: ${incident.affectedServices.map(escapeXml).join(', ')}</p>
    ${updatesHtml}
  `.trim()

  return `
    <entry>
      <id>${escapeXml(siteUrl)}/incidents#${escapeXml(incident.id)}</id>
      <title>${escapeXml(incident.title)}</title>
      <updated>${new Date(updatedAt).toISOString()}</updated>
      <link href="${escapeXml(siteUrl)}/incidents" />
      <summary type="html">${escapeXml(content)}</summary>
      <category term="${escapeXml(incident.severity)}" />
    </entry>`
}

function maintenanceToEntry(m: ScheduledMaintenance, siteUrl: string): string {
  const content = `
    <p>${escapeXml(m.description ?? 'Scheduled maintenance')}</p>
    <p>Affected: ${m.affectedServices.map(escapeXml).join(', ')}</p>
    <p>Window: ${new Date(m.scheduledStart).toISOString()} to ${new Date(m.scheduledEnd).toISOString()}</p>
  `.trim()

  return `
    <entry>
      <id>${escapeXml(siteUrl)}/incidents#maintenance-${escapeXml(m.id)}</id>
      <title>[Maintenance] ${escapeXml(m.title)}</title>
      <updated>${new Date(m.createdAt).toISOString()}</updated>
      <link href="${escapeXml(siteUrl)}" />
      <summary type="html">${escapeXml(content)}</summary>
      <category term="maintenance" />
    </entry>`
}

export async function GET() {
  const config = getSiteConfig()
  const siteUrl = config.url

  let incidents: Incident[] = []
  let maintenances: ScheduledMaintenance[] = []
  try {
    incidents = await getRecentIncidents()
  } catch {
    // DB may not be configured
  }
  try {
    maintenances = await getActiveMaintenances()
  } catch {
    // DB may not be configured
  }

  let overallStatusLabel = 'Operational'
  try {
    const services = await checkAllServices(config.services)
    const overall = calculateOverallStatus(services)
    overallStatusLabel = overall === 'operational'
      ? 'All Systems Operational'
      : overall.charAt(0).toUpperCase() + overall.slice(1)
  } catch {
    // Health check may fail
  }

  const now = new Date().toISOString()

  const incidentEntries = incidents.map(i => incidentToEntry(i, siteUrl)).join('\n')
  const maintenanceEntries = maintenances.map(m => maintenanceToEntry(m, siteUrl)).join('\n')

  const feed = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <id>${escapeXml(siteUrl)}/feed.xml</id>
  <title>${escapeXml(config.name)}</title>
  <subtitle>Current status: ${escapeXml(overallStatusLabel)}</subtitle>
  <link href="${escapeXml(siteUrl)}" />
  <link href="${escapeXml(siteUrl)}/feed.xml" rel="self" type="application/atom+xml" />
  <updated>${now}</updated>
  <author>
    <name>${escapeXml(config.name)}</name>
  </author>
${incidentEntries}
${maintenanceEntries}
</feed>`

  return new NextResponse(feed, {
    headers: {
      'Content-Type': 'application/atom+xml; charset=utf-8',
      'Cache-Control': 'public, s-maxage=300, stale-while-revalidate=60',
    },
  })
}
