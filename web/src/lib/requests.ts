// The requests made in this browser. Each holds its private key as a non-extractable Web
// Crypto key: the page can use it to open files, but no script can read its bytes, and it
// never leaves this browser. IndexedDB is the only browser storage that can keep such a key.

export interface MyRequest {
  rid: string;
  ownerToken: string;
  label: string;
  expiresAt: string;
  createdAt: string;
  privateKey: CryptoKey;
  publicKey: Uint8Array;
  names: Record<string, string>; // file id -> name, learned when a file is opened here
}

const DB = "zarf";
const STORE = "requests";

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(STORE, { keyPath: "rid" });
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function run<T>(mode: IDBTransactionMode, f: (s: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  const db = await open();
  try {
    return await new Promise<T>((resolve, reject) => {
      const req = f(db.transaction(STORE, mode).objectStore(STORE));
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
  } finally {
    db.close();
  }
}

export const saveRequest = (r: MyRequest) => run("readwrite", (s) => s.put(r));

export const forgetRequest = (rid: string) => run("readwrite", (s) => s.delete(rid));

const valid = (r: unknown): r is MyRequest => {
  const x = r as MyRequest;
  return (
    typeof x === "object" && x !== null && typeof x.rid === "string" && typeof x.ownerToken === "string" && typeof x.label === "string" &&
    typeof x.expiresAt === "string" && x.privateKey instanceof CryptoKey && x.publicKey instanceof Uint8Array
  );
};

/** Newest first. Storage is checked like any input: records of the wrong shape are skipped. */
export async function listRequests(): Promise<MyRequest[]> {
  try {
    const all = await run("readonly", (s) => s.getAll());
    return all
      .filter(valid)
      .map((r) => ({ ...r, names: typeof r.names === "object" && r.names ? r.names : {} }))
      .sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  } catch {
    return []; // storage blocked (private window, for instance): nothing to list
  }
}
