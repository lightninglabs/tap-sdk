export type Operation = "create_asset" | "issue_asset";

export type SessionStatus =
  | "staging"
  | "waiting_signature"
  | "signature_submitted"
  | "waiting_confirmation"
  | "mining"
  | "finalized"
  | "failed";

export type ExternalKey = {
  xpub: string;
  master_fingerprint: string;
  derivation_path: string;
};

export type StartSessionRequest = {
  operation: Operation;
  name: string;
  asset_ref: string;
  amount: number;
  fee_rate_sat_kw: number;
  external_key: ExternalKey;
};

export type SigningRequest = {
  operation: string;
  statement: string;
  asset_ref: string;
  issuance_ref?: string;
  name: string;
  amount: number;
  asset_type: string;
  script_key: string;
  anchor_outpoint: string;
  external_key: ExternalKey;
  virtual_psbt: string;
};

export type SessionResult = {
  asset_ref: string;
  issuance_ref?: string;
  name: string;
  amount: number;
};

export type Session = {
  id: string;
  operation: Operation;
  status: SessionStatus;
  request?: SigningRequest;
  result?: SessionResult;
  batch_key?: string;
  batch_state?: string;
  anchor_txid?: string;
  mined_blocks?: number;
  error?: string;
  created_at: string;
  updated_at: string;
};

export type CoordinatorConfig = {
  network: string;
  transport: string;
  tapd: string;
  auto_mine: boolean;
  mine_blocks: number;
};
