/** The small synchronous surface used by Pi's local persistence adapters. */
export interface DatabasePort {
  readonly path: string;
  readonly readOnly: boolean;
  db: {
    exec(sql: string): void;
    prepare(sql: string): {
      all(...parameters: unknown[]): unknown[];
      get(...parameters: unknown[]): unknown;
      run(...parameters: unknown[]): unknown;
    };
  };
  transaction<T>(fn: () => T): T;
  close(): void;
}
