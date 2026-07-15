import type { FormEvent } from 'react';

type ConnectFormProps = {
  token: string;
  onTokenChange: (token: string) => void;
  onConnect: (event: FormEvent<HTMLFormElement>) => void;
};

export function ConnectForm({ token, onTokenChange, onConnect }: ConnectFormProps) {
  return (
    <section className="max-w-lg rounded-lg border border-white/5 bg-[#1a1a1a] p-6">
      <h1 className="font-semibold text-white">Connect to the server</h1>
      <p className="mt-2 text-sm leading-6 text-slate-400">
        Enter the bearer token from your server configuration. It is kept only for this browser
        session.
      </p>
      <form onSubmit={onConnect} className="mt-5 flex flex-col gap-3 sm:flex-row">
        <label className="sr-only" htmlFor="api-token">
          API token
        </label>
        <input
          id="api-token"
          type="password"
          value={token}
          onChange={(event) => onTokenChange(event.target.value)}
          autoComplete="current-password"
          placeholder="API token"
          className="min-w-0 flex-1 rounded-md border border-white/10 bg-[#111111] px-3.5 py-2.5 text-sm text-white outline-none placeholder:text-neutral-600 focus:border-[#e5a00d]"
        />
        <button
          type="submit"
          disabled={!token.trim()}
          className="rounded-md bg-[#e5a00d] px-5 py-2.5 text-sm font-semibold text-black transition hover:bg-[#f2b51d] disabled:cursor-not-allowed disabled:opacity-40"
        >
          Connect
        </button>
      </form>
    </section>
  );
}
