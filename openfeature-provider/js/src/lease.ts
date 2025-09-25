
export type Lease<T> = [value:T, expiry:Date];


export type CachedProvider<T> = () => Promise<T>

export function leaseFactory<T>(renewer:() => Promise<Lease<T>>, marginMs = 5000):CachedProvider<T> {
  let current:Lease<T> | null = null;
  return async () => {
    if(!current || isExpired(current)) {
      const [value, expiry] = await renewer();
      current = [value, subtractMargin(expiry, marginMs)];
    }
    return current[0];
  }
}

function subtractMargin(expiry:Date, marginMs: number):Date {
  return new Date(expiry.valueOf() - marginMs)
}

function isExpired(lease:Lease<any>):boolean {
  return lease[1] < new Date();
}