import { renderToStaticMarkup } from 'react-dom/server';
import { expect, it } from 'vitest';
import { DeleteSaveDialog } from '../src/features/omnisave/delete-save-dialog.js';

it('keeps the pending save deletion visible and blocks another choice', () => {
  const markup = renderToStaticMarkup(
    <DeleteSaveDialog
      name="Save 1"
      deleting
      error=""
      onCancel={() => undefined}
      onConfirm={() => undefined}
    />
  );

  expect(markup).toContain('role="status"');
  expect(markup).toContain('Deleting…');
  expect(markup).toContain('disabled=""');
});
