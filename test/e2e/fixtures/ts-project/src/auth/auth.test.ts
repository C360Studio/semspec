import { describe, it, expect } from 'vitest';
import { AuthService } from './auth.service';

describe('AuthService', () => {
  const authService = new AuthService();

  describe('authenticate', () => {
    it('should return user and token for valid credentials', async () => {
      const result = await authService.authenticate('test@example.com', 'password');

      expect(result.user).toBeDefined();
      expect(result.user.email).toBe('test@example.com');
      expect(result.token).toBeDefined();
      expect(result.token.accessToken).toBeDefined();
    });

    it('should throw for empty email', async () => {
      await expect(authService.authenticate('', 'password')).rejects.toThrow(
        'Invalid credentials'
      );
    });

    it('should throw for empty password', async () => {
      await expect(authService.authenticate('test@example.com', '')).rejects.toThrow(
        'Invalid credentials'
      );
    });
  });

  describe('refreshToken', () => {
    it('should return new token for valid refresh token', async () => {
      const token = await authService.refreshToken('valid-refresh-token');

      expect(token).toBeDefined();
      expect(token.accessToken).toBeDefined();
      expect(token.refreshToken).toBeDefined();
    });

    it('should throw for empty refresh token', async () => {
      await expect(authService.refreshToken('')).rejects.toThrow('Refresh token required');
    });
  });
});
