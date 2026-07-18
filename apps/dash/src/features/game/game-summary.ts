import type {
  GameFingerprint,
  GameIdentifier,
  GameMedia,
  GameProvenance,
  Omnisave,
} from '../../lib/omnisave-api.js';

export type GameSummary = {
  id: string;
  label: string;
  sortKey: string;
  platform?: string;
  platformCompany?: string;
  publisher?: string;
  description?: string;
  metadataSource?: string;
  identifiers: GameIdentifier[];
  fingerprints: GameFingerprint[];
  metadata?: Record<string, unknown>;
  refreshedAt?: string;
  media: GameMedia[];
  provenance: GameProvenance[];
  saves: Omnisave[];
  inLibrary: boolean;
};
