import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { OptionsMenu } from '../src/components/options-menu.js';

describe('save download options', () => {
  it('distinguishes the current revision from the complete history', () => {
    const markup = renderToStaticMarkup(
      <OptionsMenu
        label="Save 1"
        className=""
        onDownload={() => undefined}
        onDownloadAllRevisions={() => undefined}
      />
    );

    expect(markup).toContain('Download Current');
    expect(markup).toContain('Download All');
  });
});
