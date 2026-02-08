-- Migration: Merge MacroMilestone into Milestone
-- This consolidates the separate MacroMap/MacroMilestone hierarchy into the Milestone structure

-- Step 1: Add new columns to milestones (nullable for backward compatibility)
ALTER TABLE milestones ADD COLUMN IF NOT EXISTS phase_id UUID REFERENCES phases(phase_id) ON DELETE SET NULL;
ALTER TABLE milestones ADD COLUMN IF NOT EXISTS acceptance_test TEXT;
ALTER TABLE milestones ADD COLUMN IF NOT EXISTS duration_days_min INT;
ALTER TABLE milestones ADD COLUMN IF NOT EXISTS duration_days_max INT;
ALTER TABLE milestones ADD COLUMN IF NOT EXISTS order_index INT DEFAULT 0;
ALTER TABLE milestones ADD COLUMN IF NOT EXISTS resources_forecast TEXT;

-- Step 2: Add roadmap_id to phases (direct link instead of through macro_maps)
ALTER TABLE phases ADD COLUMN IF NOT EXISTS roadmap_id UUID REFERENCES roadmaps(roadmap_id) ON DELETE CASCADE;

-- Step 3: Make old foreign keys nullable so new structure can work
-- Use DO block to handle cases where column might not exist or already be nullable
DO $$
BEGIN
    -- Make macro_map_id nullable on phases
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'phases' AND column_name = 'macro_map_id') THEN
        ALTER TABLE phases ALTER COLUMN macro_map_id DROP NOT NULL;
    END IF;

    -- Make macro_milestone_id nullable on spikes
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'spikes' AND column_name = 'macro_milestone_id') THEN
        ALTER TABLE spikes ALTER COLUMN macro_milestone_id DROP NOT NULL;
    END IF;

    -- Make macro_milestone_id nullable on risks
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'risks' AND column_name = 'macro_milestone_id') THEN
        ALTER TABLE risks ALTER COLUMN macro_milestone_id DROP NOT NULL;
    END IF;

    -- Make macro_milestone_id nullable on milestones (if it exists)
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'milestones' AND column_name = 'macro_milestone_id') THEN
        ALTER TABLE milestones ALTER COLUMN macro_milestone_id DROP NOT NULL;
    END IF;
EXCEPTION
    WHEN others THEN
        -- Ignore errors (column might already be nullable)
        NULL;
END $$;

-- Step 4: Add milestone_id to spikes and risks (new foreign key)
ALTER TABLE spikes ADD COLUMN IF NOT EXISTS milestone_id UUID REFERENCES milestones(milestone_id) ON DELETE CASCADE;
ALTER TABLE risks ADD COLUMN IF NOT EXISTS milestone_id UUID REFERENCES milestones(milestone_id) ON DELETE CASCADE;

-- Step 4: Create indexes for the new foreign keys
CREATE INDEX IF NOT EXISTS idx_milestones_phase_id ON milestones(phase_id);
CREATE INDEX IF NOT EXISTS idx_phases_roadmap_id ON phases(roadmap_id);
CREATE INDEX IF NOT EXISTS idx_spikes_milestone_id ON spikes(milestone_id);
CREATE INDEX IF NOT EXISTS idx_risks_milestone_id ON risks(milestone_id);

-- Step 5: Data migration - copy phase roadmap_id from macro_maps
UPDATE phases p
SET roadmap_id = mm.roadmap_id
FROM macro_maps mm
WHERE p.macro_map_id = mm.macro_map_id
  AND p.roadmap_id IS NULL;

-- Step 6: For existing milestones linked to macro_milestones, copy the strategic data
UPDATE milestones m
SET
    acceptance_test = COALESCE(m.acceptance_test, mmile.acceptance_test),
    duration_days_min = COALESCE(m.duration_days_min, mmile.duration_days_min),
    duration_days_max = COALESCE(m.duration_days_max, mmile.duration_days_max),
    order_index = COALESCE(m.order_index, mmile.order_index),
    resources_forecast = COALESCE(m.resources_forecast, mmile.resources_forecast),
    phase_id = mmile.phase_id
FROM macro_milestones mmile
WHERE m.macro_milestone_id = mmile.macro_milestone_id;

-- Step 7: Migrate spikes to reference milestones (for milestones linked to macro_milestones)
UPDATE spikes s
SET milestone_id = m.milestone_id
FROM milestones m
WHERE m.macro_milestone_id = s.macro_milestone_id
  AND s.milestone_id IS NULL;

-- Step 8: Migrate risks to reference milestones (for milestones linked to macro_milestones)
UPDATE risks r
SET milestone_id = m.milestone_id
FROM milestones m
WHERE m.macro_milestone_id = r.macro_milestone_id
  AND r.milestone_id IS NULL;

-- Note: Old columns (macro_map_id on phases, macro_milestone_id on milestones/spikes/risks)
-- and old tables (macro_maps, macro_milestones) are kept for now.
-- They will be dropped in a future migration after verifying the new structure works correctly.
-- This allows for rollback if needed.
