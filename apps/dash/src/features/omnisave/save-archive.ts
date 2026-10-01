import {
  downloadAllRevisionsArchive,
  downloadOmnisaveArchive,
  downloadRevisionArchive,
  type Omnisave,
  type Revision,
} from '../../lib/omnisave-api.js';

// Hands a fetched archive to the browser's own download handling.
function saveArchiveToDisk(archive: Blob, filename: string) {
  const url = URL.createObjectURL(archive);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}

// Mirrors the server's Content-Disposition stamp so browser and curl
// downloads of one revision land under the same name.
function archiveStamp(createdAt: string) {
  const date = new Date(createdAt);
  const pad = (value: number) => String(value).padStart(2, '0');
  return (
    `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())} ` +
    `${pad(date.getUTCHours())}${pad(date.getUTCMinutes())}${pad(date.getUTCSeconds())}`
  );
}

/** Downloads a save's current files as `<name>.zip`. */
export async function downloadSaveToDisk(token: string, save: Omnisave, name: string) {
  saveArchiveToDisk(await downloadOmnisaveArchive(token, save.id), `${name}.zip`);
}

/** Downloads one revision, named for its display name or else when it was committed. */
export async function downloadRevisionToDisk(
  token: string,
  save: Omnisave,
  name: string,
  revision: Revision
) {
  saveArchiveToDisk(
    await downloadRevisionArchive(token, save.id, revision.id),
    `${name} ${revision.display_name?.trim() || archiveStamp(revision.created_at)}.zip`
  );
}

/** Downloads every revision in a save's history as one archive. */
export async function downloadAllRevisionsToDisk(token: string, save: Omnisave, name: string) {
  saveArchiveToDisk(await downloadAllRevisionsArchive(token, save.id), `${name} All Revisions.zip`);
}
