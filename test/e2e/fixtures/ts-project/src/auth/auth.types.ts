export interface User {
  id: string;
  email: string;
  createdAt: Date;
}

export interface Token {
  accessToken: string;
  refreshToken: string;
  expiresAt: Date;
}

export interface AuthResult {
  user: User;
  token: Token;
}
