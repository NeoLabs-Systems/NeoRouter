import 'package:flutter/material.dart';
import 'package:neolabs_flutter/neolabs_flutter.dart'
    show
        NeoAuthCard,
        NeoAuthHeading,
        NeoAuthPage,
        NeoCredentialField,
        NeoCredentialFields,
        NeoForgotPasswordDialog,
        NeoLabeledField,
        NeoLabsPalette,
        NeoOrDivider,
        neoAuthInputDecoration,
        neoAuthLinkStyle;

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_qr_signin.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';

class NeoRouterAuthScreen extends StatefulWidget {
  const NeoRouterAuthScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<NeoRouterAuthScreen> createState() => _NeoRouterAuthScreenState();
}

class _NeoRouterAuthScreenState extends State<NeoRouterAuthScreen> {
  final TextEditingController _usernameController = TextEditingController();
  final TextEditingController _passwordController = TextEditingController();
  final TextEditingController _emailController = TextEditingController();
  final TextEditingController _confirmPasswordController =
      TextEditingController();
  final TextEditingController _codeController = TextEditingController();
  final FocusNode _usernameFocus = FocusNode();
  final FocusNode _emailFocus = FocusNode();
  final FocusNode _passwordFocus = FocusNode();
  final FocusNode _confirmFocus = FocusNode();
  late bool _registerMode = widget.controller.bootstrapRegistration;
  bool _obscure = true;

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    _emailController.dispose();
    _confirmPasswordController.dispose();
    _codeController.dispose();
    _usernameFocus.dispose();
    _emailFocus.dispose();
    _passwordFocus.dispose();
    _confirmFocus.dispose();
    super.dispose();
  }

  Future<void> _showForgotPasswordDialog() async {
    final t = AppL10n.of(context);
    final controller = widget.controller;
    final palette = neoRouterPaletteOf(context);
    final messenger = ScaffoldMessenger.of(context);
    await showDialog<void>(
      context: context,
      builder: (_) => NeoForgotPasswordDialog(
        initialAccount: _usernameController.text.trim(),
        title: t.forgotPasswordTitle,
        description: t.forgotPasswordDescription,
        accountLabel: t.usernameOrEmail,
        emptyError: t.forgotPasswordMissing,
        cancelLabel: t.cancel,
        submitLabel: t.forgotPasswordSubmit,
        onSubmit: (account) async {
          String? failure;
          try {
            await controller.auth.forgotPassword(controller.client.baseUrl, account);
          } on ApiException catch (error) {
            failure = error.message;
          } catch (_) {
            failure = t.serverUnreachable;
          }
          messenger.showSnackBar(SnackBar(content: Text(failure ?? t.forgotPasswordSent)));
          return failure == null;
        },
        errorBuilder: (message) => InlineError(message: message),
        backgroundColor: palette.surface,
        textColor: palette.textSoft,
      ),
    );
  }

  Widget _labeled(NeoLabsPalette palette, String label, Widget field, [Widget? trailing]) =>
      NeoLabeledField(palette: palette, label: label, field: field, trailing: trailing);

  Widget _credentials(NeoRouterController controller, AppL10n t) {
    final palette = neoLabsPaletteOf(context);
    return NeoCredentialFields(
      register: _registerMode,
      username: _usernameController,
      email: _emailController,
      password: _passwordController,
      confirmPassword: _confirmPasswordController,
      usernameFocus: _usernameFocus,
      emailFocus: _emailFocus,
      passwordFocus: _passwordFocus,
      confirmPasswordFocus: _confirmFocus,
      obscurePassword: _obscure,
      onChanged: (_) {},
      onSubmit: _submit,
      decoration: (NeoCredentialField field, suffix) => neoAuthInputDecoration(palette, suffixIcon: suffix),
      labeledField: (label, field, trailing) => _labeled(palette, label, field, trailing),
      usernameLabel: t.username,
      usernameOrEmailLabel: t.usernameOrEmail,
      emailLabel: t.email,
      passwordLabel: t.password,
      confirmPasswordLabel: t.confirmPassword,
      passwordToggle: IconButton(
        tooltip: _obscure ? t.showPassword : t.hidePassword,
        onPressed: () => setState(() => _obscure = !_obscure),
        icon: Icon(_obscure ? Icons.visibility_outlined : Icons.visibility_off_outlined, size: 18),
      ),
      passwordStrength: Text(t.passwordRules, style: TextStyle(color: palette.textMuted, fontSize: 12, height: 1.4)),
      forgotPassword: controller.passwordResetAvailable
          ? TextButton(
              onPressed: controller.loading ? null : _showForgotPasswordDialog,
              style: neoAuthLinkStyle(),
              child: Text(t.forgotPassword),
            )
          : null,
    );
  }

  Future<void> _submit() async {
    final t = AppL10n.of(context);
    if (_registerMode) {
      if (_passwordController.text != _confirmPasswordController.text) {
        widget.controller.showAuthError(t.passwordsDontMatch);
        return;
      }
      if (_passwordController.text.length < 12) {
        widget.controller.showAuthError(t.passwordTooShort);
        return;
      }
      await widget.controller.register(
        _usernameController.text.trim(),
        _emailController.text.trim(),
        _passwordController.text,
      );
      return;
    }
    await widget.controller.login(
      _usernameController.text.trim(),
      _passwordController.text,
    );
  }

  Future<void> _submitCode() async {
    final code = _codeController.text.trim();
    if (code.isEmpty) return;
    final ok = await widget.controller.completeTwoFactor(code);
    if (!ok) _codeController.clear();
  }

  Widget _primaryButton(NeoRouterController controller, String label, VoidCallback onPressed) => FilledButton(
    onPressed: controller.loading ? null : onPressed,
    style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(50)),
    child: controller.loading
        ? const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2))
        : Text(label),
  );

  /// Heading, password form, then the other ways in; the phone code sits above
  /// the form when it is shown.
  Widget _signInContent(NeoRouterController controller, AppL10n t) {
    final palette = neoLabsPaletteOf(context);
    final twoFactor = controller.awaitingTwoFactor;
    final bootstrap = controller.bootstrapRegistration;
    final offersOtherWays = controller.authOptionsLoaded && !_registerMode && !twoFactor;
    final error = controller.authError;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        NeoAuthHeading(
          palette: palette,
          title: twoFactor
              ? t.twoFactorLoginTitle
              : bootstrap
              ? t.authBootstrapTitle
              : (_registerMode ? t.authCreate : t.authWelcome),
          subtitle: twoFactor
              ? t.twoFactorLoginSubtitle
              : bootstrap
              ? t.authBootstrapSubtitle
              : (_registerMode ? t.authRegisterSubtitle : t.authLoginSubtitle),
        ),
        if (error != null) ...<Widget>[
          const SizedBox(height: 20),
          InlineError(message: twoFactor && controller.authErrorCode == 'INVALID_TWO_FACTOR' ? t.accountWrongCode : error),
        ],
        if (showQrSignIn && offersOtherWays) ...<Widget>[
          const SizedBox(height: 26),
          QrSignInPanel(controller: controller),
          const SizedBox(height: 22),
          NeoOrDivider(palette: palette, label: t.orUsePassword),
          const SizedBox(height: 16),
        ] else
          const SizedBox(height: 24),
        AutofillGroup(
          child: twoFactor
              ? _labeled(
                  palette,
                  t.twoFactorCode,
                  TextField(
                    controller: _codeController,
                    autofocus: true,
                    keyboardType: TextInputType.visiblePassword,
                    autofillHints: const <String>[AutofillHints.oneTimeCode],
                    onSubmitted: (_) => _submitCode(),
                    decoration: neoAuthInputDecoration(palette).copyWith(helperText: t.twoFactorCodeHint),
                  ),
                )
              : _credentials(controller, t),
        ),
        const SizedBox(height: 20),
        twoFactor
            ? _primaryButton(controller, t.twoFactorVerify, _submitCode)
            : _primaryButton(controller, _registerMode ? t.createAccount : t.signIn, _submit),
        if (twoFactor) ...<Widget>[
          const SizedBox(height: 8),
          TextButton(
            onPressed: controller.loading
                ? null
                : () {
                    _codeController.clear();
                    controller.cancelTwoFactor();
                  },
            child: Text(t.twoFactorBack),
          ),
        ] else if (offersOtherWays && controller.webAuthn.isSupported) ...<Widget>[
          const SizedBox(height: 12),
          OutlinedButton.icon(
            onPressed: controller.loading ? null : () => controller.signInWithPasskey(_usernameController.text.trim()),
            style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(46)),
            icon: const Icon(Icons.key_rounded, size: 18),
            label: Text(t.passkeySignIn),
          ),
        ],
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    final t = AppL10n.of(context);
    final palette = neoLabsPaletteOf(context);
    final compact = MediaQuery.sizeOf(context).width < AppBreakpoints.mobile;
    return NeoAuthPage(
      palette: palette,
      compact: compact,
      children: <Widget>[
        NeoAuthCard(
          palette: palette,
          compact: compact,
          mark: const BrandLockup(logoSize: 76, showName: false),
          child: _signInContent(controller, t),
        ),
        if (!controller.awaitingTwoFactor && !controller.bootstrapRegistration) ...<Widget>[
          const SizedBox(height: 16),
          TextButton(
            onPressed: controller.loading
                ? null
                : () {
                    if (!_registerMode && !controller.registrationOpen) {
                      controller.showAuthError(t.registrationClosed);
                      return;
                    }
                    setState(() => _registerMode = !_registerMode);
                  },
            child: Text(_registerMode ? t.toLogin : t.toRegister),
          ),
        ],
      ],
    );
  }
}
