import type { User, Token, AuthResult } from './auth.types';

export class AuthService {
  private users: Map<string, User> = new Map();

  async authenticate(email: string, password: string): Promise<AuthResult> {
    if (!email || !password) {
      throw new Error('Invalid credentials');
    }

    const user: User = {
      id: 'user-1',
      email,
      createdAt: new Date(),
    };

    const token = this.generateToken(user);
    return { user, token };
  }

  async refreshToken(refreshToken: string): Promise<Token> {
    if (!refreshToken) {
      throw new Error('Refresh token required');
    }
    return this.generateToken({
      id: 'user-1',
      email: 'test@example.com',
      createdAt: new Date(),
    });
  }

  private generateToken(user: User): Token {
    return {
      accessToken: `access-${user.id}`,
      refreshToken: `refresh-${user.id}`,
      expiresAt: new Date(Date.now() + 3600000),
    };
  }
}
