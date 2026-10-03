import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { SaveList } from '../src/features/omnisave/save-list.js';
import { saveScopeLabel } from '../src/features/omnisave/save-name.js';
import type { Omnisave } from '../src/lib/omnisave-api.js';

describe('save boundaries', () => {
  it('shows the save slot boundary even after a history has been renamed', () => {
    const save: Omnisave = {
      id: 'slot-history',
      game_id: 'sts2',
      display_name: 'Co-op adventures',
      scope: { kind: 'slot', adapter: 'sts2.vanilla' },
      current_revision_id: null,
      created_at: '',
      current_revision_created_at: '',
      latest_revision_created_at: '',
    };
    const markup = renderToStaticMarkup(
      <SaveList
        saves={[save]}
        expandedSaveID=""
        revisions={[]}
        achievements={[]}
        loadingRevisions={false}
        revisionError=""
        labelerAvailable={false}
        onToggleSave={() => {}}
        onPrefetchSave={() => {}}
        onDownloadSave={() => {}}
        onDownloadAllRevisions={() => {}}
        onDownloadRevision={() => {}}
        onRequestDelete={() => {}}
        onRenameSave={async () => {}}
        onRenameRevision={async () => {}}
        onRunLabeler={async () => {}}
        onOpenSave={() => {}}
        onRequestRestore={() => {}}
        onRequestFork={() => {}}
        onRequestDeleteRevision={() => {}}
      />
    );
    expect(markup).toContain('Co-op adventures');
    expect(markup).toContain('Save slot');
    expect(markup).not.toContain('Whole save');
  });
  it('retains the whole-save meaning of existing histories', () => {
    expect(saveScopeLabel({})).toBe('Whole save');
    expect(saveScopeLabel({ scope: {} })).toBe('Whole save');
  });
});
