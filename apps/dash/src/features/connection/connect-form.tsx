import { useState, type FormEvent } from 'react';

type ConnectFormProps = {
  claimable: boolean;
  pinSet: boolean;
  pending: boolean;
  error: string;
  onClaim: (pin: string) => void;
  onSignIn: (pin: string) => void;
  onOwnerToken: (token: string) => void;
};

/**
 * First contact with a server, in one of three shapes.
 *
 * A server nobody owns is claimed here, and claiming is where the owner sets
 * the PIN (ADR-010). Every browser after that signs in with those
 * four digits. The owner token is the way in from outside the network and the
 * way back when the PIN is forgotten, so it stays available and stays out of
 * the way.
 *
 * Whatever the shape, the result is the same: this browser ends up holding a
 * credential of its own, revocable beside the owner's Devices.
 */
export function ConnectForm({
  claimable,
  pinSet,
  pending,
  error,
  onClaim,
  onSignIn,
  onOwnerToken,
}: ConnectFormProps) {
  const [pin, setPin] = useState('');
  const [token, setToken] = useState('');
  const [useToken, setUseToken] = useState(false);
  const showToken = useToken || (!claimable && !pinSet);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    if (showToken) onOwnerToken(token.trim());
    else if (claimable) onClaim(pin);
    else onSignIn(pin);
  }

  return (
    <section className="max-w-lg rounded-lg border border-white/5 bg-[#1a1a1a] p-6">
      <h1 className="font-semibold text-white">
        {showToken
          ? 'Enter the owner token'
          : claimable
            ? 'This server has no owner yet'
            : 'Enter your PIN'}
      </h1>
      <p className="mt-2 text-sm leading-6 text-slate-400">
        {showToken
          ? 'The token was printed in the server log on first start. This browser trades it once for a credential of its own.'
          : claimable
            ? 'Claim it to become its owner and choose a four-digit PIN. You will use the PIN to open Omnisave from any other browser.'
            : 'Four digits, chosen when this server was claimed.'}
      </p>

      <form onSubmit={submit} className="mt-5 flex flex-col gap-3 sm:flex-row">
        {showToken ? (
          <>
            <label className="sr-only" htmlFor="owner-token">
              Owner token
            </label>
            <input
              id="owner-token"
              type="password"
              value={token}
              onChange={(event) => setToken(event.target.value)}
              autoComplete="current-password"
              placeholder="Owner token"
              className="min-w-0 flex-1 rounded-md border border-white/10 bg-[#111111] px-3.5 py-2.5 text-sm text-white outline-none placeholder:text-neutral-600 focus:border-[#e5a00d]"
            />
          </>
        ) : (
          <>
            <label className="sr-only" htmlFor="owner-pin">
              {claimable ? 'Choose a PIN' : 'PIN'}
            </label>
            <input
              id="owner-pin"
              type="password"
              inputMode="numeric"
              pattern="[0-9]*"
              maxLength={4}
              value={pin}
              onChange={(event) => setPin(event.target.value.replace(/\D/g, '').slice(0, 4))}
              autoComplete={claimable ? 'new-password' : 'current-password'}
              placeholder={claimable ? 'Choose a 4-digit PIN' : '••••'}
              className="min-w-0 flex-1 rounded-md border border-white/10 bg-[#111111] px-3.5 py-2.5 font-mono text-sm tracking-[0.4em] text-white outline-none placeholder:font-sans placeholder:tracking-normal placeholder:text-neutral-600 focus:border-[#e5a00d]"
            />
          </>
        )}
        <button
          type="submit"
          disabled={pending || (showToken ? !token.trim() : pin.length !== 4)}
          className="rounded-md bg-[#e5a00d] px-5 py-2.5 text-sm font-semibold text-black transition hover:bg-[#f2b51d] disabled:cursor-not-allowed disabled:opacity-40"
        >
          {pending ? 'Working…' : claimable && !showToken ? 'Claim this server' : 'Continue'}
        </button>
      </form>

      {error ? <p className="mt-3 text-sm text-red-300">{error}</p> : null}

      {claimable && !showToken ? null : (
        <button
          type="button"
          onClick={() => setUseToken(!showToken)}
          className="mt-4 text-xs text-neutral-500 underline underline-offset-4 transition hover:text-neutral-300"
        >
          {showToken ? 'Use the PIN instead' : 'Forgot the PIN? Use the owner token'}
        </button>
      )}
    </section>
  );
}
