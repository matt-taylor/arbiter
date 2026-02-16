-- ---------------------------------------------------------------------------
-- Arbiter Telemetry Rollup Tables
-- Phase 2: 10-second aggregation buckets
--
-- Tables are created with a single p_future catchall partition.
-- The partition maintenance script (scripts/telemetry_partitions.sh) must
-- be run after first deploy to create today's and tomorrow's partitions.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS arb_host_ip_10s (
  bucket_start INT UNSIGNED NOT NULL,
  host         VARCHAR(253) NOT NULL,
  ip           VARCHAR(45)  NOT NULL,
  total        INT UNSIGNED NOT NULL DEFAULT 0,
  c_401        INT UNSIGNED NOT NULL DEFAULT 0,
  c_403        INT UNSIGNED NOT NULL DEFAULT 0,
  c_404        INT UNSIGNED NOT NULL DEFAULT 0,
  c_429        INT UNSIGNED NOT NULL DEFAULT 0,
  c_5xx        INT UNSIGNED NOT NULL DEFAULT 0,
  m_get        INT UNSIGNED NOT NULL DEFAULT 0,
  m_post       INT UNSIGNED NOT NULL DEFAULT 0,
  m_put        INT UNSIGNED NOT NULL DEFAULT 0,
  m_patch      INT UNSIGNED NOT NULL DEFAULT 0,
  m_delete     INT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket_start, host, ip),
  INDEX idx_host_bucket (host, bucket_start)
)
ENGINE=InnoDB
PARTITION BY RANGE (bucket_start) (
  PARTITION p_future VALUES LESS THAN MAXVALUE
);

CREATE TABLE IF NOT EXISTS arb_host_ip_path_10s (
  bucket_start INT UNSIGNED NOT NULL,
  host         VARCHAR(253) NOT NULL,
  ip           VARCHAR(45)  NOT NULL,
  path_hash    BINARY(16)   NOT NULL COMMENT 'MD5 of normalized path',
  path         VARCHAR(2048) NOT NULL,
  total        INT UNSIGNED NOT NULL DEFAULT 0,
  c_401        INT UNSIGNED NOT NULL DEFAULT 0,
  c_403        INT UNSIGNED NOT NULL DEFAULT 0,
  c_404        INT UNSIGNED NOT NULL DEFAULT 0,
  c_429        INT UNSIGNED NOT NULL DEFAULT 0,
  c_5xx        INT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket_start, host, ip, path_hash),
  INDEX idx_host_ip_bucket (host, ip, bucket_start)
)
ENGINE=InnoDB
PARTITION BY RANGE (bucket_start) (
  PARTITION p_future VALUES LESS THAN MAXVALUE
);
