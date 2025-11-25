/**
 * Interface for computing SHA-256 hashes.
 * This allows mocking in tests to avoid issues with crypto.subtle and fake timers.
 */
export interface HashProvider {
  /**
   * Computes SHA-256 hash of the input string and returns hex-encoded result.
   * @param input - String to hash
   * @returns Promise that resolves to hex-encoded hash
   */
  sha256Hex(input: string): Promise<string>;
}

/**
 * Hash provider implementation using Web Crypto API.
 */
export class WebCryptoHashProvider implements HashProvider {
  async sha256Hex(input: string): Promise<string> {
    const encoder = new TextEncoder();
    const data = encoder.encode(input);
    const hashBuffer = await crypto.subtle.digest('SHA-256', data);
    const hashArray = Array.from(new Uint8Array(hashBuffer));
    const hashHex = hashArray.map(b => b.toString(16).padStart(2, '0')).join('');
    return hashHex;
  }
}
