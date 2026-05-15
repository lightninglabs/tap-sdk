import { generateMnemonic, mnemonicToSeedSync, validateMnemonic } from "@scure/bip39";
import { wordlist } from "@scure/bip39/wordlists/english.js";

import type { CoordinatorConfig } from "@/lib/types";

type Bdk = typeof import("@bitcoindevkit/bdk-wallet-web");

export type SoftwareDevice = {
  mnemonic: string;
  network: BdkNetwork;
  index: number;
  externalPrivateDescriptor: string;
  internalPrivateDescriptor: string;
  externalPublicDescriptor: string;
  internalPublicDescriptor: string;
  xpub: string;
  masterFingerprint: string;
  derivationPath: string;
};

export type SoftwareSignature = {
  signedVirtualPSBT: string;
  finalized: boolean;
};

type BdkNetwork = "bitcoin" | "testnet" | "testnet4" | "signet" | "regtest";

let bdkPromise: Promise<Bdk> | null = null;

export async function createSoftwareDevice({
  index,
  mnemonic,
  network,
}: {
  index: number;
  mnemonic?: string;
  network?: CoordinatorConfig | null;
}): Promise<SoftwareDevice> {
  if (!Number.isSafeInteger(index) || index < 0) {
    throw new Error("derivation index must be a non-negative integer");
  }

  const bdk = await loadBDK();
  const bdkNetwork = networkName(network);
  const phrase = normalizeMnemonic(mnemonic);
  const seed = mnemonicToSeedSync(phrase);
  const descriptors = bdk.seed_to_descriptor(seed, bdkNetwork, "p2tr");
  const wallet = bdk.Wallet.create(
    bdkNetwork,
    descriptors.external,
    descriptors.internal,
  );
  const externalPublicDescriptor = wallet.public_descriptor("external");
  const internalPublicDescriptor = wallet.public_descriptor("internal");
  const parsed = parseExternalDescriptor(externalPublicDescriptor, index);

  return {
    mnemonic: phrase,
    network: bdkNetwork,
    index,
    externalPrivateDescriptor: descriptors.external,
    internalPrivateDescriptor: descriptors.internal,
    externalPublicDescriptor,
    internalPublicDescriptor,
    xpub: parsed.xpub,
    masterFingerprint: parsed.masterFingerprint,
    derivationPath: parsed.derivationPath,
  };
}

export async function signWithSoftwareDevice(
  device: SoftwareDevice,
  virtualPSBT: string,
): Promise<SoftwareSignature> {
  const bdk = await loadBDK();
  const wallet = bdk.Wallet.create(
    device.network,
    device.externalPrivateDescriptor,
    device.internalPrivateDescriptor,
  );
  const psbt = bdk.Psbt.from_string(virtualPSBT);
  const options = new bdk.SignOptions();
  options.try_finalize = false;
  options.trust_witness_utxo = true;
  options.allow_all_sighashes = true;
  options.sign_with_tap_internal_key = true;

  const finalized = wallet.sign(psbt, options);

  return {
    finalized,
    signedVirtualPSBT: psbt.toString(),
  };
}

function loadBDK(): Promise<Bdk> {
  bdkPromise ??= import("@bitcoindevkit/bdk-wallet-web");

  return bdkPromise;
}

function normalizeMnemonic(mnemonic?: string): string {
  const phrase = mnemonic?.trim().replace(/\s+/g, " ");
  if (!phrase) {
    return generateMnemonic(wordlist, 128);
  }
  if (!validateMnemonic(phrase, wordlist)) {
    throw new Error("invalid BIP39 mnemonic");
  }

  return phrase;
}

function networkName(network?: CoordinatorConfig | null): BdkNetwork {
  switch (network?.network) {
    case "mainnet":
    case "bitcoin":
      return "bitcoin";
    case "testnet4":
      return "testnet4";
    case "signet":
      return "signet";
    case "testnet":
      return "testnet";
    case "regtest":
    default:
      return "regtest";
  }
}

function parseExternalDescriptor(descriptor: string, index: number) {
  const match = descriptor.match(
    /^tr\(\[([0-9a-fA-F]{8})\/([^\]]+)\]([^/)]+)\/(\d+)\/\*\)#[a-z0-9]+$/,
  );
  if (!match) {
    throw new Error(`unexpected BDK public descriptor: ${descriptor}`);
  }

  const [, masterFingerprint, accountPath, xpub, branch] = match;

  return {
    xpub,
    masterFingerprint: masterFingerprint.toLowerCase(),
    derivationPath: `m/${accountPath}/${branch}/${index}`,
  };
}
