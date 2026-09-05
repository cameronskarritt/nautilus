CREATE TABLE IF NOT EXISTS feature_flags (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,

    enabled BOOLEAN NOT NULL DEFAULT false,
    rollout_percentage FLOAT NOT NULL DEFAULT 1.0,

    created_by BIGINT REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_feature_flags_name_active ON feature_flags(name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_feature_flags_enabled ON feature_flags(enabled) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_feature_flags_active ON feature_flags(id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS feature_flag_associations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    feature_flag_id BIGINT NOT NULL REFERENCES feature_flags(id),

    object_id BIGINT NOT NULL,
    object_type TEXT NOT NULL,

    created_by BIGINT REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_feature_flag_associations_feature_flag_id ON feature_flag_associations(feature_flag_id);
CREATE INDEX IF NOT EXISTS idx_feature_flag_associations_object ON feature_flag_associations(object_id, object_type);
CREATE INDEX IF NOT EXISTS idx_feature_flag_associations_active ON feature_flag_associations(id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_feature_flag_associations_object_active
    ON feature_flag_associations(feature_flag_id, object_type, object_id)
    WHERE deleted_at IS NULL;
