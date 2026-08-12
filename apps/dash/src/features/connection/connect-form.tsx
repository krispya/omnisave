import { useState, type FormEvent } from 'react';
import { Button } from '../../components/button.js';
import { fieldClass } from '../../components/field.js';

type ConnectFormProps = {
  claimable: boolean;
  pinSet: boolean;
  pending: boolean;
  error: string;
  onClaim: (pin: string) => void;
  onSignIn: (pin: string) => void;
  onOwnerToken: (token: string) => void;
};

/** Claims a server or signs in, issuing this browser its own credential. */
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
    <section className="mx-auto mt-10 max-w-lg rounded-lg border border-outline bg-surface p-6">
      <h1 className="text-lg font-semibold text-text">
        {showToken
          ? 'Enter the owner token'
          : claimable
            ? 'This server has no owner yet'
            : 'Enter your PIN'}
      </h1>
      <p className="mt-2 text-sm leading-6 text-muted">
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
              className={`${fieldClass} flex-1`}
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
              className={`${fieldClass} flex-1 font-mono tracking-[0.4em] placeholder:font-sans placeholder:tracking-normal`}
            />
          </>
        )}
        <Button
          type="submit"
          variant="filled"
          disabled={pending || (showToken ? !token.trim() : pin.length !== 4)}
        >
          {pending ? 'Working…' : claimable && !showToken ? 'Claim this server' : 'Continue'}
        </Button>
      </form>

      {error ? (
        <p role="alert" className="mt-3 text-sm text-danger">
          {error}
        </p>
      ) : null}

      {claimable && !showToken ? null : (
        <button
          type="button"
          onClick={() => setUseToken(!showToken)}
          className="mt-4 text-xs text-muted underline underline-offset-4 transition duration-120 hover:text-text"
        >
          {showToken ? 'Use the PIN instead' : 'Forgot the PIN? Use the owner token'}
        </button>
      )}
    </section>
  );
}
