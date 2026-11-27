import {
  TRUST_LAST_REVIEWED,
  buildCommitmentsJson,
  dataStores,
  drDrill,
  rpo,
  rto,
  security,
  slaBreach,
  targetTimingLabel,
  uptime,
} from '@/lib/trust-commitments'

describe('trust-commitments', () => {
  describe('TRUST_LAST_REVIEWED', () => {
    it('is a valid ISO-8601 date', () => {
      expect(TRUST_LAST_REVIEWED).toMatch(/^\d{4}-\d{2}-\d{2}$/)
      const parsed = new Date(TRUST_LAST_REVIEWED)
      expect(Number.isNaN(parsed.getTime())).toBe(false)
    })
  })

  describe('uptime commitment', () => {
    it('publishes a conservative current monthly target', () => {
      // We deliberately don't publish 99.9% until HA ships.
      expect(uptime.monthlyTargetPercent).toBe(99.5)
    })

    it('states the lapsed 2026-06-30 goal as missed, with no new date', () => {
      expect(uptime.targetDate).toBeNull()
      expect(uptime.missedTargetDate).toBe('2026-06-30')
      expect(uptime.targetPosture.length).toBeGreaterThan(0)
      expect(targetTimingLabel(uptime)).toBe(
        '2026-06-30 target not met; no new date set'
      )
    })
  })

  describe('RPO commitments', () => {
    it('exposes current + target values for Postgres', () => {
      expect(rpo.postgres.current).toBe('24 hours')
      expect(rpo.postgres.target).toBeDefined()
      expect(rpo.postgres.targetDate).toBeNull()
      expect(rpo.postgres.missedTargetDate).toBe('2026-06-30')
    })

    it('exposes current + target values for Redis', () => {
      expect(rpo.redis.current).toContain('Hourly')
      expect(rpo.redis.target).toBeDefined()
      expect(rpo.redis.targetDate).toBeNull()
      expect(rpo.redis.missedTargetDate).toBe('2026-06-30')
    })
  })

  describe('targetTimingLabel', () => {
    it('shows a due date when one is set', () => {
      expect(targetTimingLabel({ targetDate: '2027-01-31' })).toBe('By 2027-01-31')
    })

    it('says a lapsed date was missed instead of showing it as due', () => {
      expect(
        targetTimingLabel({ targetDate: null, missedTargetDate: '2026-06-30' })
      ).toBe('2026-06-30 target not met; no new date set')
    })

    it('says no date is set when there is neither', () => {
      expect(targetTimingLabel({})).toBe('No date set')
    })
  })

  describe('RTO commitments', () => {
    it('pod-level RTO is labelled "current" (automatic)', () => {
      expect(rto.singlePod.tone).toBe('current')
      expect(rto.singlePod.current).toContain('2 minutes')
    })

    it('control-plane RTO is labelled "pending" until first drill', () => {
      expect(rto.controlPlane.tone).toBe('pending')
      // Drill scheduled, value is not yet proven in practice.
      expect(rto.controlPlane.currentNotes).toContain('Untested')
    })
  })

  describe('DR drill', () => {
    it('declares first drill pending (lastDrillDate null)', () => {
      expect(drDrill.lastDrillDate).toBeNull()
    })

    it('has a scheduled next-drill date', () => {
      expect(drDrill.nextDrillDate).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    })
  })

  describe('data stores', () => {
    it('includes the four canonical stores', () => {
      const names = dataStores.map((s) => s.store)
      expect(names).toContain('PostgreSQL')
      expect(names).toContain('Redis')
      expect(names).toContain('Cloudflare R2 (blob storage)')
      expect(names).toContain('Kubernetes manifests')
    })

    it('every store has backup, retention, and a status note', () => {
      for (const store of dataStores) {
        expect(store.backup).toBeTruthy()
        expect(store.retention).toBeTruthy()
        expect(store.statusNote).toBeTruthy()
      }
    })
  })

  describe('security items', () => {
    it('has at least one "current", one "target", and one "pending" item', () => {
      const tones = new Set(security.map((s) => s.tone))
      expect(tones.has('current')).toBe(true)
      expect(tones.has('target')).toBe(true)
      expect(tones.has('pending')).toBe(true)
    })

    it('SOC 2 commitment explicitly names Type I, not Type II', () => {
      const soc = security.find((s) => s.label.includes('SOC 2'))
      expect(soc).toBeDefined()
      expect(soc!.status).toContain('Type I')
    })
  })

  describe('slaBreach', () => {
    it('has a contact address and response window', () => {
      expect(slaBreach.contact).toMatch(/^[^@\s]+@[^@\s]+\.[^@\s]+$/)
      expect(slaBreach.responseWindow).toBeTruthy()
    })
  })

  describe('buildCommitmentsJson', () => {
    const payload = buildCommitmentsJson()

    it('returns RPO values in minutes matching the human-readable commitments', () => {
      expect(payload.rpo.postgres.current_minutes).toBe(24 * 60)
      expect(payload.rpo.postgres.target_minutes).toBe(15)
      expect(payload.rpo.redis.current_minutes).toBe(60)
    })

    it('publishes lapsed target dates as null, with the missed date alongside', () => {
      expect(payload.uptime.goal_target_date).toBeNull()
      expect(payload.uptime.goal_target_date_missed).toBe('2026-06-30')
      expect(payload.rpo.postgres.target_date).toBeNull()
      expect(payload.rpo.postgres.target_date_missed).toBe('2026-06-30')
      expect(payload.rpo.redis.target_date).toBeNull()
      expect(payload.rpo.redis.target_date_missed).toBe('2026-06-30')
    })

    it('returns RTO values in seconds/minutes', () => {
      expect(payload.rto.single_pod_failure_seconds).toBe(120)
      expect(payload.rto.control_plane_failure_minutes_current_max).toBe(240)
      expect(payload.rto.full_rebuild_minutes_max).toBe(240)
    })

    it('serialises to valid JSON', () => {
      expect(() => JSON.stringify(payload)).not.toThrow()
    })

    it('includes last_reviewed and data_export contact', () => {
      expect(payload.last_reviewed).toBe(TRUST_LAST_REVIEWED)
      expect(payload.data_export.contact).toBe('sla@madfam.io')
      expect(payload.data_export.window_hours_max).toBe(72)
    })

    it('mirrors data_stores length', () => {
      expect(payload.data_stores.length).toBe(dataStores.length)
    })
  })
})
