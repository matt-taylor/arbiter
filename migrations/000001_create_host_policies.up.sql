CREATE TABLE host_policies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  host TEXT NOT NULL UNIQUE,
  killswitch_required INTEGER NOT NULL,
  gatekeeper_required INTEGER NOT NULL,
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_host_policies_host ON host_policies(host);
