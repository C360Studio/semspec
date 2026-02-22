/**
 * User interface for authentication.
 */
export interface User {
  id: number;
  name: string;
  email: string;
}

/**
 * Authenticates a user with the given credentials.
 */
export async function authenticate(username: string, password: string): Promise<User | null> {
  // Mock authentication
  if (username === 'admin' && password === 'secret') {
    return { id: 1, name: 'Admin', email: 'admin@example.com' };
  }
  return null;
}

/**
 * AuthService handles user authentication.
 */
export class AuthService {
  private users: User[] = [];

  /**
   * Validates user credentials.
   */
  async validate(token: string): Promise<boolean> {
    return token.length > 0;
  }
}
