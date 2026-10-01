import { useCallback, useEffect, useState } from 'react';
import {
  approvePairingRequest,
  denyPairingRequest,
  listPairingRequests,
  type PairingRequest,
} from '../../lib/omnisave-api.js';

/**
 * Pairing requests waiting on the owner, and whether the dialog that answers
 * them is showing. A new request opens it on its own; the owner can also open
 * it deliberately.
 */
export function usePairingRequests(token: string) {
  const [pending, setPending] = useState<PairingRequest[]>([]);
  const [answering, setAnswering] = useState('');
  const [error, setError] = useState('');
  const [dismissed, setDismissed] = useState<string[]>([]);
  // Manual opening may show an empty request list.
  const [requestsOpen, setRequestsOpen] = useState(false);

  const refresh = useCallback(async () => {
    if (!token) return;
    try {
      setPending(await listPairingRequests(token));
    } catch {
      // Ignore unreadable requests and wait for the next event.
    }
  }, [token]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function answer(request: PairingRequest, approve: boolean) {
    setAnswering(request.id);
    setError('');
    try {
      await (approve
        ? approvePairingRequest(token, request.id)
        : denyPairingRequest(token, request.id));
      await refresh();
    } catch (answerError) {
      setError(answerError instanceof Error ? answerError.message : 'That did not work.');
      await refresh();
    } finally {
      setAnswering('');
    }
  }

  // Dismissal leaves the request pending; a later request may interrupt again.
  const unanswered = pending.filter((request) => !dismissed.includes(request.id));

  return {
    /** Every live request, dismissed ones included. */
    pending,
    /** Opened deliberately, or interrupted by a request not yet dismissed. */
    open: requestsOpen || unanswered.length > 0,
    /** What the dialog lists: every live request when opened deliberately. */
    requests: requestsOpen ? pending : unanswered,
    /** The request being answered, or empty. */
    answering,
    error,
    /** Re-reads the live requests; runs on its own whenever the token changes. */
    refresh,
    openRequests: () => setRequestsOpen(true),
    answer,
    dismiss: () => {
      setRequestsOpen(false);
      setDismissed(pending.map((request) => request.id));
    },
  };
}
