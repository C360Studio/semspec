import { AuthService } from './auth/auth.service';
import type { AuthResult } from './auth/auth.types';

async function main(): Promise<void> {
  const authService = new AuthService();

  try {
    const result: AuthResult = await authService.authenticate('admin@example.com', 'secret');
    console.log('Welcome', result.user.email);
    console.log('Token expires at:', result.token.expiresAt);
  } catch (error) {
    console.error('Auth failed:', error);
  }
}

main();

export { AuthService } from './auth/auth.service';
export type { User, Token, AuthResult } from './auth/auth.types';
