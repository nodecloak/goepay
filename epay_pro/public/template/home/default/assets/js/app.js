;(function ($, window, document) {
  'use strict';

  function toggleMobileNav(event) {
    if (event) {
      event.preventDefault();
      event.stopPropagation();
    }

    $('.activity-box').toggleClass('hide');
    $('body').toggleClass('no-scrolling');
    $('.mobile-nav').toggleClass('show');
    $('.title-bar').toggleClass('title-bar--active');

    return false;
  }

  function initMobileNav() {
    var $toggle = $('.title-bar__toggle');
    if (!$toggle.length) return;

    $toggle.off('touchstart click').on('touchstart click', toggleMobileNav);

    $(window).on('resize', function () {
      if ($(window).width() > 640) {
        $('.mobile-nav').removeClass('show');
        $('.title-bar').removeClass('title-bar--active');
        $('body').removeClass('no-scrolling');
      }
    });
  }

  function initMobileFooterAccordion() {
    $('.nav-title').on('click', function () {
      $(this).siblings('.nav-content').toggleClass('expand');
    });
  }

  function initServiceScrollEffects() {
    if (!$('#scroll1').length || !$('#scroll2').length || !$('.home-header-container').length) {
      return;
    }

    var topH = $('.home-header-container').height() || 0;

    $(document).on('scroll', function () {
      var scroH = $(document).scrollTop();
      var scroll1 = $('#scroll1').offset().top;
      var scroll2 = $('#scroll2').offset().top;
      var halfHeight = (scroll2 - scroll1) / 2;

      if (scroH < topH) {
        $('.left1').addClass('active').siblings().removeClass('active');
        $('.scroll1').addClass('active').siblings().removeClass('active');
        $('.left-content').css({ position: 'absolute', top: '0', paddingLeft: '16.67%' });
        $('.right-bg').css({ position: 'absolute', top: '0', right: '0' });
      }

      if (scroH >= topH + 55) {
        $('.left-content').css({ position: 'fixed', top: '55px', paddingLeft: '16.67%' });
        $('.right-bg').css({ position: 'fixed', top: '55px', background: '#C8F4F9' });
        $('.dashed1,.dashed2').css({ background: '#C8F4F9' });
        $('.circle-bg').css({ background: 'linear-gradient(335deg, rgba(72, 191, 177, 1) 0%, rgba(71, 189, 171, 1) 100%)' });
      }

      if (scroH >= scroll1 + halfHeight && scroH < scroll2 + halfHeight) {
        $('.left2').addClass('active').siblings().removeClass('active');
        $('.scroll2').addClass('active').siblings().removeClass('active');
        $('.right-bg').css({ background: '#F0EAFF' });
        $('.dashed1,.dashed2').css({ background: '#F0EAFF' });
        $('.circle-bg').css({ background: 'linear-gradient(327deg, rgba(204, 180, 237, 1) 0%, rgba(225, 176, 198, 1) 100%)' });
      }

      if (scroH >= scroll2) {
        $('.left-content').css({ position: 'absolute', bottom: '0', top: 'auto', paddingLeft: '16.67%' });
        $('.right-bg').css({ position: 'absolute', right: '0', bottom: '0', top: 'auto', background: '#C9E4FF' });
        $('.circle-bg').css({ background: 'linear-gradient(327deg, rgba(124, 166, 255, 1) 0%, rgba(177, 184, 242, 1) 100%)' });
        $('.dashed1,.dashed2').css({ background: '#C9E4FF' });
      }
    });
  }

  $(function () {
    initMobileNav();
    initMobileFooterAccordion();
    initServiceScrollEffects();
  });
})(window.jQuery, window, document);

(function () {
  var qrItems = document.querySelectorAll('.contact-qr-wrap');
  for (var i = 0; i < qrItems.length; i++) {
      var item = qrItems[i];
      var link = item.getAttribute('data-qr-link');
      var qrContainer = item.querySelector('.contact-qr-canvas');
      if (!link || !qrContainer || typeof QRCode === 'undefined') {
          continue;
      }
      qrContainer.innerHTML = '';
      new QRCode(qrContainer, {
          text: link,
          width: 112,
          height: 112
      });
  }
})();