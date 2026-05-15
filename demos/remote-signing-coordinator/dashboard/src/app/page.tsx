"use client";

import {
  AlertTriangle,
  BadgeCheck,
  Boxes,
  CheckCircle2,
  Clipboard,
  Clock,
  Copy,
  Cpu,
  Database,
  FileSignature,
  Fingerprint,
  Hash,
  KeyRound,
  Loader2,
  Network,
  Play,
  RefreshCw,
  Route,
  Send,
  Server,
  ShieldCheck,
  WandSparkles,
  WalletCards,
} from "lucide-react";
import type { ReactNode } from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  ApiError,
  createSession,
  getConfig,
  getSession,
  getSessions,
  submitSignature,
} from "@/lib/api";
import { formatAmount, formatTime, shortValue, statusLabel } from "@/lib/format";
import {
  createSoftwareDevice,
  signWithSoftwareDevice,
  type SoftwareDevice,
} from "@/lib/softwareDevice";
import type {
  CoordinatorConfig,
  ExternalKey,
  Operation,
  Session,
  SessionStatus,
  SigningRequest,
} from "@/lib/types";

type FormState = {
  operation: Operation;
  name: string;
  assetRef: string;
  amount: string;
  feeRateSatVByte: string;
  xpub: string;
  masterFingerprint: string;
  derivationPath: string;
};

type KnownAsset = {
  assetRef: string;
  name: string;
  supply: number;
  updatedAt: string;
  externalKey?: ExternalKey;
};

const initialForm: FormState = {
  operation: "create_asset",
  name: "demo-usd",
  assetRef: "",
  amount: "100000",
  feeRateSatVByte: "1.1",
  xpub: "",
  masterFingerprint: "",
  derivationPath: "m/86'/1'/0'/0/0",
};

const terminalStatuses: SessionStatus[] = ["finalized", "failed"];
const minManualFeeRateSatKw = 253;
const feeRateSatKwPerSatVByte = 250;

export default function Dashboard() {
  const [config, setConfig] = useState<CoordinatorConfig | null>(null);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(initialForm);
  const [device, setDevice] = useState<SoftwareDevice | null>(null);
  const [signedPSBT, setSignedPSBT] = useState("");
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const selectedSession = useMemo(() => {
    return sessions.find((session) => session.id === selectedID) ?? null;
  }, [selectedID, sessions]);
  const knownAssets = useMemo(() => summarizeAssets(sessions), [sessions]);

  const refresh = useCallback(async () => {
    setRefreshing(true);
    try {
      const [nextConfig, nextSessions] = await Promise.all([
        getConfig(),
        getSessions(),
      ]);
      setConfig(nextConfig);
      setSessions(sortSessions(nextSessions));
      setError(null);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      void refresh();
    }, 0);

    return () => window.clearTimeout(timeout);
  }, [refresh]);

  useEffect(() => {
    const interval = window.setInterval(() => {
      void refresh();
    }, 2500);

    return () => window.clearInterval(interval);
  }, [refresh]);

  useEffect(() => {
    if (!selectedSession || terminalStatuses.includes(selectedSession.status)) {
      return;
    }

    const interval = window.setInterval(async () => {
      try {
        const next = await getSession(selectedSession.id);
        setSessions((current) => upsertSession(current, next));
      } catch (err) {
        setError(errorMessage(err));
      }
    }, 1200);

    return () => window.clearInterval(interval);
  }, [selectedSession]);

  async function handleStart() {
    setLoading(true);
    setError(null);
    setSignedPSBT("");

    try {
      const session = await createSession({
        operation: form.operation,
        name: form.operation === "create_asset" ? form.name.trim() : "",
        asset_ref:
          form.operation === "issue_asset" ? form.assetRef.trim() : "",
        amount: Number(form.amount),
        fee_rate_sat_kw: feeRateSatVByteToSatKw(form.feeRateSatVByte),
        external_key: {
          xpub: form.xpub.trim(),
          master_fingerprint: form.masterFingerprint.trim(),
          derivation_path: form.derivationPath.trim(),
        },
      });
      setSessions((current) => upsertSession(current, session));
      setSelectedID(session.id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }

  async function handleSignatureSubmit() {
    if (!selectedSession) {
      return;
    }

    setLoading(true);
    setError(null);
    try {
      const session = await submitSignature(selectedSession.id, signedPSBT);
      setSessions((current) => upsertSession(current, session));
      setSignedPSBT("");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="min-h-screen px-4 py-5 text-zinc-100 sm:px-6 lg:px-8">
      <div className="mx-auto flex max-w-7xl flex-col gap-5">
        <header className="flex flex-col gap-4 border-b border-white/10 pb-5 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-md border border-orange-400/35 bg-orange-500/15 text-orange-200">
              <ShieldCheck className="h-5 w-5" aria-hidden />
            </div>
            <div>
              <h1 className="text-2xl font-semibold text-white">
                Remote Issuance Coordinator
              </h1>
              <p className="text-sm text-zinc-400">
                Taproot Assets SDK demo for externally signed Issuances.
              </p>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <StatusPill
              icon={<Network className="h-4 w-4" aria-hidden />}
              label={config?.network ?? "network"}
            />
            <StatusPill
              icon={<Server className="h-4 w-4" aria-hidden />}
              label={config ? `${config.transport} ${config.tapd}` : "tapd"}
            />
            {config?.auto_mine && (
              <StatusPill
                icon={<Cpu className="h-4 w-4" aria-hidden />}
                label={`auto-mines ${config.mine_blocks} regtest blocks`}
              />
            )}
            <button
              className="inline-flex h-10 items-center gap-2 rounded-md border border-white/10 bg-zinc-900 px-3 text-sm text-zinc-200 transition hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-60"
              disabled={refreshing}
              onClick={() => void refresh()}
              type="button"
            >
              <RefreshCw
                className={`h-4 w-4 ${refreshing ? "animate-spin" : ""}`}
                aria-hidden
              />
              Refresh
            </button>
          </div>
        </header>

        {error && (
          <div className="flex items-start gap-3 rounded-md border border-red-500/35 bg-red-500/10 px-4 py-3 text-sm text-red-100">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
            <span>{error}</span>
          </div>
        )}

        <div className="grid gap-5 lg:grid-cols-[390px_minmax(0,1fr)]">
          <div className="flex flex-col gap-5">
            <Panel title="Start Issuance" icon={<Play className="h-4 w-4" />}>
              <IssuanceForm
                form={form}
                knownAssets={knownAssets}
                loading={loading}
                onChange={setForm}
                onStart={() => void handleStart()}
              />
            </Panel>

            <Panel title="Software Device" icon={<Cpu className="h-4 w-4" />}>
              <SoftwareDevicePanel
                config={config}
                device={device}
                form={form}
                selectedSession={selectedSession}
                onDevice={setDevice}
                onError={setError}
                onForm={setForm}
                onSignedPSBT={setSignedPSBT}
              />
            </Panel>

            <Panel
              title="Coordinator Runs"
              icon={<Clock className="h-4 w-4" />}
            >
              <SessionList
                sessions={sessions}
                selectedID={selectedID}
                onSelect={setSelectedID}
              />
            </Panel>
          </div>

          <Panel
            title="Signing Review"
            icon={<FileSignature className="h-4 w-4" />}
            className="min-h-[720px]"
          >
            <SigningWorkspace
              loading={loading}
              session={selectedSession}
              signedPSBT={signedPSBT}
              onCopyError={setError}
              onSignedPSBT={setSignedPSBT}
              onSubmit={() => void handleSignatureSubmit()}
            />
          </Panel>
        </div>
      </div>
    </main>
  );
}

function SoftwareDevicePanel({
  config,
  device,
  form,
  onDevice,
  onError,
  onForm,
  onSignedPSBT,
  selectedSession,
}: {
  config: CoordinatorConfig | null;
  device: SoftwareDevice | null;
  form: FormState;
  onDevice: (device: SoftwareDevice | null) => void;
  onError: (error: string | null) => void;
  onForm: (form: FormState) => void;
  onSignedPSBT: (value: string) => void;
  selectedSession: Session | null;
}) {
  const [index, setIndex] = useState("0");
  const [mnemonic, setMnemonic] = useState("");
  const [busy, setBusy] = useState(false);
  const loadID = useRef(0);

  async function createDevice(nextMnemonic?: string, nextIndex = index) {
    const currentLoadID = loadID.current + 1;
    loadID.current = currentLoadID;
    setBusy(true);
    onError(null);

    try {
      const nextDevice = await createSoftwareDevice({
        index: Number(nextIndex || "0"),
        mnemonic: nextMnemonic,
        network: config,
      });
      if (currentLoadID !== loadID.current) {
        return;
      }
      onDevice(nextDevice);
      setMnemonic(nextDevice.mnemonic);
      onForm({
        ...form,
        xpub: nextDevice.xpub,
        masterFingerprint: nextDevice.masterFingerprint,
        derivationPath: nextDevice.derivationPath,
      });
    } catch (err) {
      onError(errorMessage(err));
    } finally {
      if (currentLoadID === loadID.current) {
        setBusy(false);
      }
    }
  }

  function handleIndexChange(nextIndex: string) {
    setIndex(nextIndex);
    if (!device || !mnemonic.trim()) {
      return;
    }

    void createDevice(mnemonic, nextIndex);
  }

  async function sign() {
    if (!device || !selectedSession?.request) {
      return;
    }

    setBusy(true);
    onError(null);
    try {
      const signature = await signWithSoftwareDevice(
        device,
        selectedSession.request.virtual_psbt,
      );
      onSignedPSBT(signature.signedVirtualPSBT);
    } catch (err) {
      onError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-3 sm:grid-cols-[1fr_96px]">
        <TextAreaField
          label="Mnemonic"
          onChange={setMnemonic}
          value={mnemonic}
        />
        <Field
          label="Index"
          onChange={handleIndexChange}
          type="number"
          value={index}
        />
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        <button
          className="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-white/10 bg-zinc-950 px-3 text-sm text-zinc-200 transition hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-60"
          disabled={busy}
          onClick={() => void createDevice()}
          type="button"
        >
          {busy ? (
            <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
          ) : (
            <WandSparkles className="h-4 w-4" aria-hidden />
          )}
          Generate
        </button>
        <button
          className="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-white/10 bg-zinc-950 px-3 text-sm text-zinc-200 transition hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-60"
          disabled={busy || !mnemonic.trim()}
          onClick={() => void createDevice(mnemonic)}
          type="button"
        >
          <KeyRound className="h-4 w-4" aria-hidden />
          Import
        </button>
      </div>

      {device && (
        <div className="rounded-md border border-white/10 bg-zinc-950/80 p-3">
          <ReviewFact
            icon={<FileSignature className="h-4 w-4" aria-hidden />}
            label="Public descriptor"
            title={device.externalPublicDescriptor}
            value={shortValue(device.externalPublicDescriptor, 22, 16)}
          />
          <div className="mt-3 grid gap-3 sm:grid-cols-2">
            <ReviewFact
              icon={<Fingerprint className="h-4 w-4" aria-hidden />}
              label="Fingerprint"
              value={device.masterFingerprint}
            />
            <ReviewFact
              icon={<Route className="h-4 w-4" aria-hidden />}
              label="Derivation"
              value={device.derivationPath}
            />
          </div>
        </div>
      )}

      <button
        className="inline-flex h-10 items-center justify-center gap-2 rounded-md bg-emerald-400 px-4 text-sm font-semibold text-zinc-950 transition hover:bg-emerald-300 disabled:cursor-not-allowed disabled:bg-zinc-700 disabled:text-zinc-400"
        disabled={
          busy ||
          !device ||
          selectedSession?.status !== "waiting_signature" ||
          !selectedSession.request
        }
        onClick={() => void sign()}
        type="button"
      >
        {busy ? (
          <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
        ) : (
          <FileSignature className="h-4 w-4" aria-hidden />
        )}
        Sign Request
      </button>
    </div>
  );
}

function IssuanceForm({
  form,
  knownAssets,
  loading,
  onChange,
  onStart,
}: {
  form: FormState;
  knownAssets: KnownAsset[];
  loading: boolean;
  onChange: (form: FormState) => void;
  onStart: () => void;
}) {
  const feeRateSatVByte = Number(form.feeRateSatVByte || "0");
  const feeRateSatKw = feeRateSatVByteToSatKw(form.feeRateSatVByte);
  const feeRateInvalid =
    Number.isNaN(feeRateSatVByte) ||
    feeRateSatVByte < 0 ||
    (feeRateSatKw > 0 && feeRateSatKw < minManualFeeRateSatKw);
  const disabled =
    loading ||
    !form.xpub.trim() ||
    !form.masterFingerprint.trim() ||
    !form.derivationPath.trim() ||
    Number(form.amount) <= 0 ||
    feeRateInvalid ||
    (form.operation === "create_asset" && !form.name.trim()) ||
    (form.operation === "issue_asset" && !form.assetRef.trim());

  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 rounded-md border border-white/10 bg-zinc-950 p-1">
        <ModeButton
          active={form.operation === "create_asset"}
          icon={<Boxes className="h-4 w-4" aria-hidden />}
          label="Create Asset"
          onClick={() => onChange({ ...form, operation: "create_asset" })}
        />
        <ModeButton
          active={form.operation === "issue_asset"}
          icon={<BadgeCheck className="h-4 w-4" aria-hidden />}
          label="New Issuance"
          onClick={() => onChange({ ...form, operation: "issue_asset" })}
        />
      </div>

      {form.operation === "create_asset" ? (
        <Field
          label="Asset name"
          value={form.name}
          onChange={(name) => onChange({ ...form, name })}
        />
      ) : (
        <div className="flex flex-col gap-3">
          {knownAssets.length > 0 && (
            <SelectField
              label="Existing Asset"
              value={form.assetRef}
              onChange={(assetRef) => {
                const knownAsset = knownAssets.find(
                  (asset) => asset.assetRef === assetRef,
                );
                const externalKey = knownAsset?.externalKey;

                onChange({
                  ...form,
                  assetRef,
                  ...(externalKey
                    ? {
                        xpub: externalKey.xpub,
                        masterFingerprint:
                          externalKey.master_fingerprint,
                        derivationPath: externalKey.derivation_path,
                      }
                    : {}),
                });
              }}
            >
              <option value="">Paste AssetRef</option>
              {knownAssets.map((asset) => (
                <option key={asset.assetRef} value={asset.assetRef}>
                  {asset.name || "Asset"} -{" "}
                  {shortValue(asset.assetRef, 10, 6)} -{" "}
                  {formatAmount(asset.supply)} units
                </option>
              ))}
            </SelectField>
          )}
          <TextAreaField
            label="AssetRef"
            value={form.assetRef}
            onChange={(assetRef) => onChange({ ...form, assetRef })}
          />
        </div>
      )}

      <div className="grid gap-3 sm:grid-cols-2">
        <Field
          label="Amount (base units)"
          type="number"
          value={form.amount}
          onChange={(amount) => onChange({ ...form, amount })}
        />
        <Field
          label="Fee rate (sat/vB)"
          min="0"
          placeholder="tapd default"
          step="0.1"
          type="number"
          value={form.feeRateSatVByte}
          onChange={(feeRateSatVByte) =>
            onChange({ ...form, feeRateSatVByte })
          }
        />
      </div>
      <div className="rounded-md border border-white/10 bg-zinc-950/60 px-3 py-2 text-xs text-zinc-400">
        {feeRateSatKw === 0 ? (
          "Using tapd's default anchor fee rate"
        ) : feeRateSatKw < minManualFeeRateSatKw ? (
          "Minimum manual fee rate is 1.02 sat/vB"
        ) : (
          `${feeRateSatKw} sat/kWU sent to the SDK`
        )}
      </div>

      <div className="rounded-md border border-white/10 bg-zinc-950/80 p-3">
        <div className="mb-3 flex items-center gap-2 text-sm font-medium text-zinc-200">
          <KeyRound className="h-4 w-4 text-orange-300" aria-hidden />
          Issuance key descriptor
        </div>
        <div className="flex flex-col gap-3">
          <TextAreaField
            label="Account xpub"
            value={form.xpub}
            onChange={(xpub) => onChange({ ...form, xpub })}
          />
          <div className="grid gap-3 sm:grid-cols-2">
            <Field
              label="Master fingerprint"
              placeholder="f23f9fd2"
              value={form.masterFingerprint}
              onChange={(masterFingerprint) =>
                onChange({ ...form, masterFingerprint })
              }
            />
            <Field
              label="Derivation path"
              value={form.derivationPath}
              onChange={(derivationPath) =>
                onChange({ ...form, derivationPath })
              }
            />
          </div>
        </div>
      </div>

      <button
        className="inline-flex h-11 items-center justify-center gap-2 rounded-md bg-orange-500 px-4 text-sm font-semibold text-zinc-950 transition hover:bg-orange-400 disabled:cursor-not-allowed disabled:bg-zinc-700 disabled:text-zinc-400"
        disabled={disabled}
        onClick={onStart}
        type="button"
      >
        {loading ? (
          <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
        ) : (
          <Play className="h-4 w-4" aria-hidden />
        )}
        Stage Issuance
      </button>
    </div>
  );
}

function SigningWorkspace({
  loading,
  session,
  signedPSBT,
  onCopyError,
  onSignedPSBT,
  onSubmit,
}: {
  loading: boolean;
  session: Session | null;
  signedPSBT: string;
  onCopyError: (error: string | null) => void;
  onSignedPSBT: (value: string) => void;
  onSubmit: () => void;
}) {
  if (!session) {
    return (
      <div className="flex min-h-[610px] items-center justify-center rounded-md border border-dashed border-white/10 bg-zinc-950/40 p-8 text-center">
        <div className="max-w-md">
          <WalletCards className="mx-auto mb-4 h-10 w-10 text-zinc-500" />
          <h2 className="text-lg font-semibold text-zinc-100">
            No run selected
          </h2>
          <p className="mt-2 text-sm leading-6 text-zinc-400">
            Start an Issuance or select a previous run to review the
            signing payload.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-5">
      <div className="rounded-md border border-white/10 bg-zinc-950/70 p-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="text-sm text-zinc-500">Run</div>
            <div className="font-mono text-sm text-zinc-100">{session.id}</div>
          </div>
          <SessionStatusBadge status={session.status} />
        </div>
        <StatusStepper status={session.status} />
      </div>

      {session.error && (
        <div className="rounded-md border border-red-500/35 bg-red-500/10 p-4 text-sm text-red-100">
          {session.error}
        </div>
      )}

      {session.request ? (
        <RequestReview
          request={session.request}
          onCopyError={onCopyError}
          status={session.status}
        />
      ) : (
        <div className="rounded-md border border-white/10 bg-zinc-950/60 p-6">
          <div className="flex items-center gap-3 text-sm text-zinc-300">
            <Loader2 className="h-4 w-4 animate-spin text-orange-300" />
            Building funded Issuance payload in tapd.
          </div>
        </div>
      )}

      {session.request && session.status === "waiting_signature" && (
        <div className="rounded-md border border-white/10 bg-zinc-950/70 p-4">
          <div className="mb-3 flex items-center justify-between gap-3">
            <div>
              <h3 className="text-sm font-semibold text-white">
                Signed virtual PSBT
              </h3>
              <p className="mt-1 text-sm text-zinc-400">
                Paste the externally signed payload returned by the signer.
              </p>
            </div>
            <Send className="h-5 w-5 text-orange-300" aria-hidden />
          </div>
          <textarea
            className="min-h-32 w-full resize-y rounded-md border border-white/10 bg-black/35 p-3 font-mono text-xs text-zinc-100 outline-none transition placeholder:text-zinc-600 focus:border-orange-400/80"
            onChange={(event) => onSignedPSBT(event.target.value)}
            placeholder="base64 signed virtual PSBT"
            value={signedPSBT}
          />
          <button
            className="mt-3 inline-flex h-10 items-center justify-center gap-2 rounded-md bg-emerald-400 px-4 text-sm font-semibold text-zinc-950 transition hover:bg-emerald-300 disabled:cursor-not-allowed disabled:bg-zinc-700 disabled:text-zinc-400"
            disabled={
              loading ||
              !signedPSBT.trim() ||
              session.status !== "waiting_signature"
            }
            onClick={onSubmit}
            type="button"
          >
            {loading ? (
              <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
            ) : (
              <CheckCircle2 className="h-4 w-4" aria-hidden />
            )}
            Submit Signature
          </button>
        </div>
      )}

      {session.result && (
        <ResultPanel session={session} onCopyError={onCopyError} />
      )}
    </div>
  );
}

function RequestReview({
  request,
  onCopyError,
  status,
}: {
  request: SigningRequest;
  onCopyError: (error: string | null) => void;
  status: SessionStatus;
}) {
  const facts = [
    {
      icon: <FileSignature className="h-4 w-4" aria-hidden />,
      label: "Authorization",
      value: request.statement,
    },
    {
      icon: <Hash className="h-4 w-4" aria-hidden />,
      label: "AssetRef",
      copyValue: request.asset_ref,
      onCopyError,
      value: shortValue(request.asset_ref, 14, 12),
      title: request.asset_ref,
    },
    {
      icon: <Database className="h-4 w-4" aria-hidden />,
      label: "Amount increases supply by",
      value: formatAmount(request.amount),
    },
    {
      icon: <KeyRound className="h-4 w-4" aria-hidden />,
      label: "Issuance key",
      value: shortValue(request.external_key.xpub, 16, 10),
      title: request.external_key.xpub,
    },
    {
      icon: <WalletCards className="h-4 w-4" aria-hidden />,
      label: "Script key controls minted Asset",
      value: shortValue(request.script_key, 14, 12),
      title: request.script_key,
    },
    {
      icon: <Clipboard className="h-4 w-4" aria-hidden />,
      label: "Anchor outpoint commits this Issuance",
      value: request.anchor_outpoint || "pending",
    },
  ];

  return (
    <div className="rounded-md border border-white/10 bg-zinc-950/70 p-4">
      <div className="mb-4 flex items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold text-white">
            Issuance payload
          </h3>
          <p className="mt-1 text-sm text-zinc-400">
            Review these fields before the external signer signs.
          </p>
        </div>
        <SessionStatusBadge status={status} />
      </div>

      <div className="grid gap-3 md:grid-cols-2">
        {facts.map((fact) => (
          <ReviewFact key={fact.label} {...fact} />
        ))}
      </div>

      <div className="mt-4 grid gap-3 md:grid-cols-2">
        <ReviewFact
          icon={<Fingerprint className="h-4 w-4" aria-hidden />}
          label="Master fingerprint"
          value={request.external_key.master_fingerprint}
        />
        <ReviewFact
          icon={<Route className="h-4 w-4" aria-hidden />}
          label="Derivation path"
          value={request.external_key.derivation_path}
        />
      </div>

      <div className="mt-5 rounded-md border border-white/10 bg-black/35">
        <div className="flex items-center justify-between border-b border-white/10 px-3 py-2">
          <span className="text-xs font-medium uppercase text-zinc-500">
            Unsigned virtual PSBT
          </span>
          <CopyButton
            text={request.virtual_psbt}
            title="Copy unsigned virtual PSBT"
            onError={onCopyError}
          />
        </div>
        <pre className="max-h-60 overflow-auto p-3 font-mono text-xs leading-5 text-zinc-300">
          {request.virtual_psbt}
        </pre>
      </div>
    </div>
  );
}

function ResultPanel({
  onCopyError,
  session,
}: {
  onCopyError: (error: string | null) => void;
  session: Session;
}) {
  const result = session.result;
  if (!result) {
    return null;
  }
  const finalized = session.status === "finalized";
  const issuanceRef = result.issuance_ref ?? session.request?.issuance_ref;

  return (
    <div
      className={`rounded-md border p-4 ${
        finalized
          ? "border-emerald-400/30 bg-emerald-400/10"
          : "border-orange-400/30 bg-orange-400/10"
      }`}
    >
      <div
        className={`mb-3 flex items-center gap-2 text-sm font-semibold ${
          finalized ? "text-emerald-100" : "text-orange-100"
        }`}
      >
        {finalized ? (
          <CheckCircle2 className="h-4 w-4" aria-hidden />
        ) : (
          <Clock className="h-4 w-4" aria-hidden />
        )}
        {finalized ? "Finalized" : "Waiting for batch confirmation"}
      </div>
      <div className="grid gap-3 md:grid-cols-3">
        <ReviewFact
          copyValue={result.asset_ref}
          icon={<Hash className="h-4 w-4" aria-hidden />}
          label="AssetRef"
          onCopyError={onCopyError}
          value={shortValue(result.asset_ref, 14, 12)}
          title={result.asset_ref}
        />
        <ReviewFact
          copyValue={issuanceRef}
          icon={<FileSignature className="h-4 w-4" aria-hidden />}
          label="Issuance Ref"
          onCopyError={onCopyError}
          value={
            issuanceRef
              ? shortValue(issuanceRef, 12, 10)
              : finalized
                ? "Complete"
                : "pending"
          }
          title={issuanceRef}
        />
        <ReviewFact
          icon={<Database className="h-4 w-4" aria-hidden />}
          label="Amount"
          value={formatAmount(result.amount)}
        />
      </div>
      {(session.batch_state || session.batch_key || session.anchor_txid) && (
        <div className="mt-3 grid gap-3 md:grid-cols-3">
          <ReviewFact
            icon={<Clock className="h-4 w-4" aria-hidden />}
            label="Mint batch state"
            value={session.batch_state ?? "pending"}
          />
          <ReviewFact
            copyValue={session.batch_key}
            icon={<KeyRound className="h-4 w-4" aria-hidden />}
            label="Mint batch"
            onCopyError={onCopyError}
            value={shortValue(session.batch_key, 12, 10)}
            title={session.batch_key}
          />
          <ReviewFact
            copyValue={session.anchor_txid}
            icon={<Hash className="h-4 w-4" aria-hidden />}
            label="Anchor txid"
            onCopyError={onCopyError}
            value={shortValue(session.anchor_txid, 12, 10)}
            title={session.anchor_txid}
          />
        </div>
      )}
      {session.mined_blocks ? (
        <div className="mt-3 text-xs text-zinc-400">
          Mined {session.mined_blocks} regtest blocks for confirmation.
        </div>
      ) : null}
    </div>
  );
}

function SessionList({
  sessions,
  selectedID,
  onSelect,
}: {
  sessions: Session[];
  selectedID: string | null;
  onSelect: (id: string) => void;
}) {
  if (sessions.length === 0) {
    return (
      <div className="rounded-md border border-dashed border-white/10 bg-zinc-950/50 p-5 text-sm text-zinc-400">
        No Issuance runs yet.
      </div>
    );
  }

  return (
    <div className="flex max-h-[430px] flex-col gap-2 overflow-auto pr-1">
      {sessions.map((session) => (
        <button
          className={`rounded-md border p-3 text-left transition ${
            selectedID === session.id
              ? "border-orange-400/55 bg-orange-400/10"
              : "border-white/10 bg-zinc-950/70 hover:bg-zinc-900"
          }`}
          key={session.id}
          onClick={() => onSelect(session.id)}
          type="button"
        >
          <div className="flex items-center justify-between gap-3">
            <span className="font-mono text-sm text-zinc-100">
              {shortValue(session.id, 8, 4)}
            </span>
            <SessionStatusBadge status={session.status} />
          </div>
          <div className="mt-2 flex items-center justify-between gap-3 text-xs text-zinc-500">
            <span>{session.operation.replace("_", " ")}</span>
            <span>{formatTime(session.updated_at)}</span>
          </div>
        </button>
      ))}
    </div>
  );
}

function StatusStepper({ status }: { status: SessionStatus }) {
  const steps: Array<{ keys: SessionStatus[]; label: string }> = [
    { keys: ["staging"], label: "Stage" },
    { keys: ["waiting_signature"], label: "Review" },
    { keys: ["signature_submitted"], label: "Submit" },
    { keys: ["waiting_confirmation", "mining"], label: "Confirm" },
    { keys: ["finalized"], label: "Finalize" },
  ];
  const activeIndex = steps.findIndex((step) => step.keys.includes(status));

  return (
    <div className="mt-5 grid grid-cols-5 gap-2">
      {steps.map((step, index) => {
        const active =
          status === "failed"
            ? index <= Math.max(activeIndex, 0)
            : index <= activeIndex;
        return (
          <div key={step.label}>
            <div
              className={`h-1.5 rounded-full ${
                active ? "bg-orange-400" : "bg-zinc-800"
              }`}
            />
            <div className="mt-2 text-xs text-zinc-500">{step.label}</div>
          </div>
        );
      })}
    </div>
  );
}

function Panel({
  children,
  className = "",
  icon,
  title,
}: {
  children: ReactNode;
  className?: string;
  icon: ReactNode;
  title: string;
}) {
  return (
    <section
      className={`rounded-md border border-white/10 bg-zinc-900/70 shadow-2xl shadow-black/25 backdrop-blur ${className}`}
    >
      <div className="flex items-center gap-2 border-b border-white/10 px-4 py-3 text-sm font-semibold text-zinc-100">
        <span className="text-orange-300">{icon}</span>
        {title}
      </div>
      <div className="p-4">{children}</div>
    </section>
  );
}

function Field({
  label,
  min,
  onChange,
  placeholder,
  step,
  type = "text",
  value,
}: {
  label: string;
  min?: string;
  onChange: (value: string) => void;
  placeholder?: string;
  step?: string;
  type?: string;
  value: string;
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-xs font-medium uppercase text-zinc-500">
        {label}
      </span>
      <input
        className="h-10 w-full rounded-md border border-white/10 bg-black/35 px-3 text-sm text-zinc-100 outline-none transition placeholder:text-zinc-600 focus:border-orange-400/80"
        onChange={(event) => onChange(event.target.value)}
        min={min}
        placeholder={placeholder}
        step={step}
        type={type}
        value={value}
      />
    </label>
  );
}

function TextAreaField({
  label,
  onChange,
  value,
}: {
  label: string;
  onChange: (value: string) => void;
  value: string;
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-xs font-medium uppercase text-zinc-500">
        {label}
      </span>
      <textarea
        className="min-h-24 w-full resize-y rounded-md border border-white/10 bg-black/35 px-3 py-2 font-mono text-xs text-zinc-100 outline-none transition placeholder:text-zinc-600 focus:border-orange-400/80"
        onChange={(event) => onChange(event.target.value)}
        value={value}
      />
    </label>
  );
}

function SelectField({
  children,
  label,
  onChange,
  value,
}: {
  children: ReactNode;
  label: string;
  onChange: (value: string) => void;
  value: string;
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-xs font-medium uppercase text-zinc-500">
        {label}
      </span>
      <select
        className="h-10 w-full rounded-md border border-white/10 bg-black/35 px-3 text-sm text-zinc-100 outline-none transition focus:border-orange-400/80"
        onChange={(event) => onChange(event.target.value)}
        value={value}
      >
        {children}
      </select>
    </label>
  );
}

function ModeButton({
  active,
  icon,
  label,
  onClick,
}: {
  active: boolean;
  icon: ReactNode;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      className={`inline-flex h-10 items-center justify-center gap-2 rounded-md px-3 text-sm transition ${
        active
          ? "bg-orange-500 text-zinc-950"
          : "text-zinc-400 hover:bg-zinc-900 hover:text-zinc-100"
      }`}
      onClick={onClick}
      type="button"
    >
      {icon}
      {label}
    </button>
  );
}

function ReviewFact({
  copyValue,
  icon,
  label,
  onCopyError,
  title,
  value,
}: {
  copyValue?: string;
  icon: ReactNode;
  label: string;
  onCopyError?: (error: string | null) => void;
  title?: string;
  value?: string;
}) {
  return (
    <div className="min-w-0 rounded-md border border-white/10 bg-zinc-900/80 p-3">
      <div className="mb-2 flex items-center gap-2 text-xs font-medium uppercase text-zinc-500">
        <span className="text-orange-300">{icon}</span>
        {label}
      </div>
      <div className="flex items-start justify-between gap-2">
        <div
          className="min-w-0 break-words font-mono text-sm leading-6 text-zinc-100"
          title={title}
        >
          {value || "pending"}
        </div>
        {copyValue && onCopyError && (
          <CopyButton
            text={copyValue}
            title={`Copy ${label}`}
            onError={onCopyError}
          />
        )}
      </div>
    </div>
  );
}

function StatusPill({
  icon,
  label,
}: {
  icon: ReactNode;
  label: string;
}) {
  return (
    <div className="inline-flex h-10 items-center gap-2 rounded-md border border-white/10 bg-zinc-900 px-3 text-sm text-zinc-300">
      <span className="text-orange-300">{icon}</span>
      <span className="max-w-[240px] truncate">{label}</span>
    </div>
  );
}

function SessionStatusBadge({ status }: { status: SessionStatus }) {
  const classes =
    status === "finalized"
      ? "border-emerald-400/35 bg-emerald-400/10 text-emerald-200"
      : status === "failed"
        ? "border-red-400/35 bg-red-400/10 text-red-200"
        : "border-orange-400/35 bg-orange-400/10 text-orange-200";

  return (
    <span
      className={`inline-flex h-7 items-center rounded-md border px-2 text-xs font-medium ${classes}`}
    >
      {statusLabel(status)}
    </span>
  );
}

function CopyButton({
  onError,
  text,
  title = "Copy",
}: {
  onError: (error: string | null) => void;
  text: string;
  title?: string;
}) {
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      onError(null);
    } catch (err) {
      onError(errorMessage(err));
    }
  }

  return (
    <button
      className="inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-400 transition hover:bg-white/10 hover:text-zinc-100"
      onClick={() => void copy()}
      title={title}
      type="button"
    >
      <Copy className="h-4 w-4" aria-hidden />
    </button>
  );
}

function upsertSession(current: Session[], next: Session): Session[] {
  const exists = current.some((session) => session.id === next.id);
  if (!exists) {
    return sortSessions([next, ...current]);
  }

  return sortSessions(
    current.map((session) => (session.id === next.id ? next : session)),
  );
}

function sortSessions(sessions: Session[]): Session[] {
  return [...sessions].sort(
    (a, b) =>
      new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
  );
}

function summarizeAssets(sessions: Session[]): KnownAsset[] {
  const byRef = new Map<string, KnownAsset>();

  for (const session of sessions) {
    if (session.status !== "finalized" || !session.result?.asset_ref) {
      continue;
    }

    const assetRef = session.result.asset_ref;
    const current = byRef.get(assetRef);
    byRef.set(assetRef, {
      assetRef,
      externalKey: session.request?.external_key ?? current?.externalKey,
      name: session.result.name || current?.name || "",
      supply: (current?.supply ?? 0) + session.result.amount,
      updatedAt: newerTimestamp(current?.updatedAt, session.updated_at),
    });
  }

  return [...byRef.values()].sort(
    (a, b) =>
      new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime(),
  );
}

function newerTimestamp(a = "", b = ""): string {
  return new Date(a).getTime() > new Date(b).getTime() ? a : b;
}

function feeRateSatVByteToSatKw(value: string): number {
  const feeRateSatVByte = Number(value || "0");
  if (!Number.isFinite(feeRateSatVByte) || feeRateSatVByte <= 0) {
    return 0;
  }

  return Math.ceil(feeRateSatVByte * feeRateSatKwPerSatVByte);
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message;
  }
  if (err instanceof Error) {
    return err.message;
  }

  return "Unexpected error";
}
