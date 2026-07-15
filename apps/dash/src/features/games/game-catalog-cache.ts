import { use } from 'react';
import { listGames, type CatalogGame } from '../../lib/omnisave-api.js';

type CatalogGamePromise = Promise<CatalogGame | null>;

const catalogLists = new Map<string, Promise<CatalogGame[]>>();
const catalogGames = new Map<string, Map<string, CatalogGamePromise>>();

function catalogList(token: string) {
  let promise = catalogLists.get(token);
  if (!promise) {
    promise = listGames(token).catch(() => []);
    catalogLists.set(token, promise);
  }
  return promise;
}

export function catalogGamePromise(token: string, gameID: string): CatalogGamePromise {
  let games = catalogGames.get(token);
  if (!games) {
    games = new Map();
    catalogGames.set(token, games);
  }

  let promise = games.get(gameID);
  if (!promise) {
    promise = catalogList(token).then(
      (catalog) => catalog.find((game) => game.id === gameID) ?? null
    );
    games.set(gameID, promise);
  }
  return promise;
}

export function useCatalogGame(token: string, gameID: string) {
  return use(catalogGamePromise(token, gameID));
}

export function primeCatalogGame(token: string, gameID: string, promise: CatalogGamePromise) {
  let games = catalogGames.get(token);
  if (!games) {
    games = new Map();
    catalogGames.set(token, games);
  }
  games.set(gameID, promise);
}

export function clearCatalogCache(token: string) {
  catalogLists.delete(token);
  catalogGames.delete(token);
}
