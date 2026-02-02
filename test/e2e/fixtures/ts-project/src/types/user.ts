// Re-export user types from auth for convenience
export type { User } from '../auth/auth.types';

// Extended user profile type
export interface UserProfile {
  user: import('../auth/auth.types').User;
  displayName: string;
  avatarUrl?: string;
  preferences: UserPreferences;
}

export interface UserPreferences {
  theme: 'light' | 'dark';
  notifications: boolean;
  language: string;
}
