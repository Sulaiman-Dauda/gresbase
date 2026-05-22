/// Authentication service for Gresbase Dart SDK.
library gresbase_sdk_auth;

import 'dart:async';
import 'dart:convert';
import 'http_client.dart';
import 'models.dart';

/// Callback type for auth events.
typedef AuthEventHandler = void Function(AuthResponse? data);

/// Authentication service with login, register, OAuth, OTP, magic link, and token management.
class AuthService {
  final HttpClient _http;
  String? _token;
  String? _refreshToken;
  AdminUser? _admin;
  Future<String?>? _refreshPromise;

  final List<AuthEventHandler> _onLogin = [];
  final List<AuthEventHandler> _onLogout = [];
  final List<AuthEventHandler> _onRefresh = [];

  AuthService(this._http);

  /// Current auth token.
  String? get token => _token;

  /// Current refresh token.
  String? get refreshToken => _refreshToken;

  /// Currently authenticated admin user.
  AdminUser? get admin => _admin;

  /// Whether the client is authenticated.
  bool get isAuthenticated => _token != null;

  /// Listen for login events.
  void onLogin(AuthEventHandler handler) => _onLogin.add(handler);

  /// Listen for logout events.
  void onLogout(AuthEventHandler handler) => _onLogout.add(handler);

  /// Listen for token refresh events.
  void onRefreshEvent(AuthEventHandler handler) => _onRefresh.add(handler);

  /// Login with email and password.
  Future<AuthResponse> login(String email, String password) async {
    final result = await _http.request('POST', '/api/v1/auth/login',
        body: {'email': email, 'password': password});
    final response = AuthResponse.fromJson(result);
    _token = response.token;
    _refreshToken = response.refreshToken;
    _admin = response.admin;
    _notify(_onLogin, response);
    return response;
  }

  /// Register a new admin user.
  Future<AuthResponse> register(
      String email, String password, {String role = 'admin'}) async {
    final result = await _http.request('POST', '/api/v1/auth/register',
        body: {'email': email, 'password': password, 'role': role});
    final response = AuthResponse.fromJson(result);
    _token = response.token;
    _refreshToken = response.refreshToken;
    _admin = response.admin;
    _notify(_onLogin, response);
    return response;
  }

  /// Refresh the access token.
  Future<String?> refresh() async {
    if (_refreshToken == null) return null;
    if (_refreshPromise != null) return _refreshPromise;

    _refreshPromise = _doRefresh();
    try {
      return await _refreshPromise;
    } finally {
      _refreshPromise = null;
    }
  }

  Future<String?> _doRefresh() async {
    try {
      final result = await _http.request('POST', '/api/v1/auth/refresh',
          body: {'refreshToken': _refreshToken});
      _token = result['token'] as String?;
      _refreshToken = result['refreshToken'] as String?;
      _notify(_onRefresh, null);
      return _token;
    } catch (_) {
      await logout(serverLogout: false);
      return null;
    }
  }

  /// Logout and clear session.
  Future<void> logout({bool serverLogout = true}) async {
    try {
      if (serverLogout && _token != null) {
        await _http.request('POST', '/api/v1/auth/logout');
      }
    } catch (_) {}
    _token = null;
    _refreshToken = null;
    _admin = null;
    _notify(_onLogout, null);
  }

  /// Request OTP code.
  Future<Map<String, dynamic>> requestOTP(String email) async {
    return _http.request('POST', '/api/v1/auth/otp/request', body: {'email': email});
  }

  /// Verify OTP code and login.
  Future<AuthResponse> verifyOTP(String otpId, String code) async {
    final result = await _http.request('POST', '/api/v1/auth/otp/verify',
        body: {'otpId': otpId, 'code': code});
    final response = AuthResponse.fromJson(result);
    _token = response.token;
    _refreshToken = response.refreshToken;
    _admin = response.admin;
    _notify(_onLogin, response);
    return response;
  }

  /// Request magic link.
  Future<void> requestMagicLink(String email) async {
    await _http.request('POST', '/api/v1/auth/magic-link', body: {'email': email});
  }

  /// Verify magic link token and login.
  Future<AuthResponse> verifyMagicLink(String token) async {
    final result = await _http.request('POST', '/api/v1/auth/magic-link/verify',
        body: {'token': token});
    final response = AuthResponse.fromJson(result);
    _token = response.token;
    _refreshToken = response.refreshToken;
    _admin = response.admin;
    _notify(_onLogin, response);
    return response;
  }

  /// Get OAuth authorization URL.
  String oAuthURL(String provider, {String? redirectUrl}) {
    var url = '${_http.baseUrl}/api/v1/oauth/$provider';
    if (redirectUrl != null) {
      url += '?redirectUrl=${Uri.encodeComponent(redirectUrl)}';
    }
    return url;
  }

  /// Request password reset.
  Future<void> requestPasswordReset(String email) async {
    await _http.request('POST', '/api/v1/admin/password-reset',
        body: {'email': email});
  }

  /// Confirm password reset.
  Future<void> confirmPasswordReset(String token, String newPassword) async {
    await _http.request('POST', '/api/v1/admin/password-reset/confirm',
        body: {'token': token, 'newPassword': newPassword});
  }

  /// Get current admin profile.
  Future<AdminUser> getMe() async {
    final result =
        await _http.request('GET', '/api/v1/admin/me');
    _admin = AdminUser.fromJson(result);
    return _admin!;
  }

  /// Update current admin profile.
  Future<AdminUser> updateMe(Map<String, dynamic> data) async {
    final result =
        await _http.request('PUT', '/api/v1/admin/me', body: data);
    _admin = AdminUser.fromJson(result);
    return _admin!;
  }

  void _notify(List<AuthEventHandler> handlers, AuthResponse? data) {
    for (final handler in handlers) {
      handler(data);
    }
  }
}
