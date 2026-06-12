/// Authentication service: admin auth + shared token store.
library;

import 'dart:async';

import 'http_client.dart';
import 'models.dart';

/// Callback invoked whenever the auth state changes.
///
/// Receives a JSON-serializable snapshot (`token`, `refreshToken`, `admin`,
/// `record`) that can be persisted (e.g. shared_preferences, secure storage)
/// and later restored via [AuthService.restore]. All values are `null` after
/// logout.
typedef AuthPersistCallback = void Function(Map<String, dynamic> authData);

/// Admin authentication and shared token store.
///
/// The token store is in-memory; pass an [AuthPersistCallback] to mirror
/// auth state to durable storage, and call [restore] on startup.
/// Both admin auth (`client.auth.login`) and record auth
/// (`client.collection('users').authWithPassword`) share this store, so all
/// subsequent requests and realtime connections are authenticated.
class AuthService {
  final GresbaseHttpClient _http;
  final AuthPersistCallback? _onChange;

  String? _token;
  String? _refreshToken;
  AdminUser? _admin;
  RecordData? _record;
  Future<String?>? _refreshFuture;

  final _events = StreamController<String>.broadcast();

  AuthService(this._http, {AuthPersistCallback? onChange})
      : _onChange = onChange;

  /// Current access token (null when unauthenticated).
  String? get token => _token;

  /// Current refresh token.
  String? get refreshToken => _refreshToken;

  /// The authenticated admin user (set by admin login).
  AdminUser? get admin => _admin;

  /// The authenticated collection record (set by record auth).
  RecordData? get record => _record;

  /// Whether the client holds an access token.
  bool get isAuthenticated => _token != null && _token!.isNotEmpty;

  /// Auth lifecycle events: `login`, `register`, `refresh`, `logout`.
  Stream<String> get events => _events.stream;

  void _notify(String event) {
    _onChange?.call({
      'token': _token,
      'refreshToken': _refreshToken,
      'admin': _admin?.toJson(),
      'record': _record,
    });
    if (!_events.isClosed) _events.add(event);
  }

  /// Manually set the auth tokens (e.g. from a server-issued token).
  void setToken(String? token, [String? refreshToken]) {
    _token = token;
    if (refreshToken != null) _refreshToken = refreshToken;
    _notify('change');
  }

  /// Restore a previously persisted auth snapshot (see [AuthPersistCallback]).
  void restore(Map<String, dynamic> authData) {
    _token = authData['token'] as String?;
    _refreshToken = authData['refreshToken'] as String?;
    final adminJson = authData['admin'];
    _admin = adminJson is Map<String, dynamic>
        ? AdminUser.fromJson(adminJson)
        : null;
    final recordJson = authData['record'];
    _record = recordJson is Map<String, dynamic> ? recordJson : null;
  }

  /// Store a record-auth result (token + refresh token + auth record) in the
  /// shared token store. Used by record auth methods so that record auth and
  /// admin auth share the exact same plumbing.
  void setRecordAuth(RecordAuthResponse result) {
    _token = result.token;
    _refreshToken = result.refreshToken;
    _record = result.record;
    _notify('login');
  }

  /// Login with admin email and password.
  Future<AuthResponse> login(String email, String password) async {
    final result = await _http.send('POST', '/api/v1/auth/login',
        body: {'email': email, 'password': password});
    final response = AuthResponse.fromJson(result as Map<String, dynamic>);
    _token = response.token;
    _refreshToken = response.refreshToken;
    _admin = response.admin;
    _record = null;
    _notify('login');
    return response;
  }

  /// Register a new admin user.
  Future<AuthResponse> register(String email, String password,
      {String role = 'admin'}) async {
    final result = await _http.send('POST', '/api/v1/auth/register',
        body: {'email': email, 'password': password, 'role': role});
    final response = AuthResponse.fromJson(result as Map<String, dynamic>);
    _token = response.token;
    _refreshToken = response.refreshToken;
    _admin = response.admin;
    _record = null;
    _notify('register');
    return response;
  }

  /// Refresh the access token using the stored refresh token.
  ///
  /// Returns the new access token, or null when no refresh token is stored
  /// or the refresh fails (in which case local auth state is cleared).
  /// Concurrent calls are deduplicated.
  Future<String?> refresh() {
    if (_refreshToken == null || _refreshToken!.isEmpty) {
      return Future.value(null);
    }
    final inFlight = _refreshFuture;
    if (inFlight != null) return inFlight;

    final future = _doRefresh();
    _refreshFuture = future;
    return future;
  }

  Future<String?> _doRefresh() async {
    try {
      final result = await _http.send('POST', '/api/v1/auth/refresh',
          body: {'refreshToken': _refreshToken}, retryOn401: false);
      final data = result as Map<String, dynamic>;
      _token = data['token'] as String?;
      _refreshToken = data['refreshToken'] as String? ?? _refreshToken;
      _notify('refresh');
      return _token;
    } catch (_) {
      await logout(serverLogout: false);
      return null;
    } finally {
      _refreshFuture = null;
    }
  }

  /// Logout and clear the local session.
  Future<void> logout({bool serverLogout = true}) async {
    try {
      if (serverLogout && isAuthenticated) {
        await _http.send('POST', '/api/v1/auth/logout', body: const {});
      }
    } catch (_) {
      // Always clear local state.
    }
    _token = null;
    _refreshToken = null;
    _admin = null;
    _record = null;
    _notify('logout');
  }

  /// Request an OTP code.
  Future<Map<String, dynamic>> requestOTP(String email) async {
    final result = await _http
        .send('POST', '/api/v1/auth/otp/request', body: {'email': email});
    return (result as Map<String, dynamic>?) ?? {};
  }

  /// Verify an OTP code and login.
  Future<AuthResponse> verifyOTP(String otpId, String code) async {
    final result = await _http.send('POST', '/api/v1/auth/otp/verify',
        body: {'otpId': otpId, 'code': code});
    final response = AuthResponse.fromJson(result as Map<String, dynamic>);
    _token = response.token;
    _refreshToken = response.refreshToken;
    _admin = response.admin;
    _record = null;
    _notify('login');
    return response;
  }

  /// Request a magic link email.
  Future<void> requestMagicLink(String email) async {
    await _http.send('POST', '/api/v1/auth/magic-link', body: {'email': email});
  }

  /// Verify a magic link token and login.
  Future<AuthResponse> verifyMagicLink(String token) async {
    final result = await _http
        .send('POST', '/api/v1/auth/magic-link/verify', body: {'token': token});
    final response = AuthResponse.fromJson(result as Map<String, dynamic>);
    _token = response.token;
    _refreshToken = response.refreshToken;
    _admin = response.admin;
    _record = null;
    _notify('login');
    return response;
  }

  /// Build the OAuth login redirect URL for a provider.
  String oAuthUrl(String provider, {String? redirectUrl}) {
    var url = '${_http.baseUrl}/api/v1/oauth/$provider';
    if (redirectUrl != null) {
      url += '?redirectUrl=${Uri.encodeQueryComponent(redirectUrl)}';
    }
    return url;
  }

  /// Request an admin password reset email.
  Future<void> requestPasswordReset(String email) async {
    await _http
        .send('POST', '/api/v1/admin/password-reset', body: {'email': email});
  }

  /// Confirm an admin password reset.
  Future<void> confirmPasswordReset(String token, String newPassword) async {
    await _http.send('POST', '/api/v1/admin/password-reset/confirm',
        body: {'token': token, 'newPassword': newPassword});
  }

  /// Get the current admin profile.
  Future<AdminUser> getMe() async {
    final result = await _http.send('GET', '/api/v1/admin/me');
    _admin = AdminUser.fromJson(result as Map<String, dynamic>);
    _notify('change');
    return _admin!;
  }

  /// Update the current admin profile.
  Future<AdminUser> updateMe(Map<String, dynamic> data) async {
    final result = await _http.send('PUT', '/api/v1/admin/me', body: data);
    _admin = AdminUser.fromJson(result as Map<String, dynamic>);
    _notify('change');
    return _admin!;
  }

  /// Release resources held by the auth event stream.
  void dispose() {
    _events.close();
  }
}
