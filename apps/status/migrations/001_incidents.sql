-- Status page incident management schema
-- Apply with: psql $DATABASE_URL -f migrations/001_incidents.sql

CREATE TABLE IF NOT EXISTS incidents (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title VARCHAR(255) NOT NULL,
  status VARCHAR(20) NOT NULL DEFAULT 'investigating',
  severity VARCHAR(20) NOT NULL,
  affected_services TEXT[] NOT NULL DEFAULT '{}',
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  resolved_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS incident_updates (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  message TEXT NOT NULL,
  status VARCHAR(20),
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS scheduled_maintenance (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title VARCHAR(255) NOT NULL,
  description TEXT,
  affected_services TEXT[] NOT NULL DEFAULT '{}',
  scheduled_start TIMESTAMP WITH TIME ZONE NOT NULL,
  scheduled_end TIMESTAMP WITH TIME ZONE NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_incidents_status ON incidents(status);
CREATE INDEX IF NOT EXISTS idx_incidents_created_at ON incidents(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_incidents_severity ON incidents(severity);
CREATE INDEX IF NOT EXISTS idx_incident_updates_incident_id ON incident_updates(incident_id);
CREATE INDEX IF NOT EXISTS idx_incident_updates_created_at ON incident_updates(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_scheduled_maintenance_start ON scheduled_maintenance(scheduled_start);
CREATE INDEX IF NOT EXISTS idx_scheduled_maintenance_end ON scheduled_maintenance(scheduled_end);
