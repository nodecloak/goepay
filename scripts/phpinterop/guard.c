/* Test-only ELF constructor. Compile in the existing idle runner, never install
 * globally. policy.h is generated from the same pinned policy.json as Go. */
#if !defined(__x86_64__) || defined(__ILP32__)
#error Linux amd64 LP64 is required
#endif
#include <linux/audit.h>
#include <linux/filter.h>
#include <linux/seccomp.h>
#include <sys/prctl.h>
#include <sys/syscall.h>
#include "policy.h"

extern char **environ;
static long call6(long n,long a,long b,long c,long d,long e,long f) {
  register long r10 __asm__("r10")=d;
  register long r8 __asm__("r8")=e;
  register long r9 __asm__("r9")=f;
  long result;
  __asm__ volatile("syscall":"=a"(result):"a"(n),"D"(a),"S"(b),"d"(c),"r"(r10),"r"(r8),"r"(r9):"rcx","r11","memory");
  return result;
}
static unsigned length(const char *s){unsigned n=0;while(s[n])n++;return n;}
static int prefix(const char *s,const char *p){while(*p){if(*s++!=*p++)return 0;}return 1;}
static void fail(void){const char *s="PHP_GUARD_UNAVAILABLE\n";call6(SYS_write,2,(long)s,length(s),0,0,0);call6(SYS_exit_group,126,0,0,0,0,0);}

__attribute__((constructor)) static void restrict_php(void) {
  const char *nonce=0;
  char *state=0;
  for(char **p=environ;*p;p++)if(prefix(*p,"GOEPAY_PHP_GUARD_NONCE="))nonce=*p+length("GOEPAY_PHP_GUARD_NONCE=");
  for(char **p=environ;*p;p++)if(prefix(*p,"GOEPAY_PHP_GUARD_STATE="))state=*p+length("GOEPAY_PHP_GUARD_STATE=");
  if(!nonce||length(nonce)!=32)fail();
  if(!state||!prefix(state,"pending")||length(state)!=7)fail();
  for(unsigned i=0;i<32;i++)if(!((nonce[i]>='0'&&nonce[i]<='9')||(nonce[i]>='a'&&nonce[i]<='f')))fail();
  if(call6(SYS_geteuid,0,0,0,0,0,0)!=65534)fail();
  struct sock_filter filters[6+2*GOEPAY_POLICY_COUNT+1];
  unsigned at=0;
  filters[at++]=(struct sock_filter)BPF_STMT(BPF_LD|BPF_W|BPF_ABS,4);
  filters[at++]=(struct sock_filter)BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,AUDIT_ARCH_X86_64,1,0);
  filters[at++]=(struct sock_filter)BPF_STMT(BPF_RET|BPF_K,SECCOMP_RET_KILL_PROCESS);
  filters[at++]=(struct sock_filter)BPF_STMT(BPF_LD|BPF_W|BPF_ABS,0);
  filters[at++]=(struct sock_filter)BPF_JUMP(BPF_JMP|BPF_JGE|BPF_K,0x40000000,0,1);
  filters[at++]=(struct sock_filter)BPF_STMT(BPF_RET|BPF_K,SECCOMP_RET_ERRNO|1);
  for(unsigned i=0;i<GOEPAY_POLICY_COUNT;i++) {
    filters[at++]=(struct sock_filter)BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,goepay_denied[i],0,1);
    filters[at++]=(struct sock_filter)BPF_STMT(BPF_RET|BPF_K,SECCOMP_RET_ERRNO|1);
  }
  filters[at++]=(struct sock_filter)BPF_STMT(BPF_RET|BPF_K,SECCOMP_RET_ALLOW);
  struct sock_fprog program={(unsigned short)at,filters};
  if(call6(SYS_prctl,PR_SET_NO_NEW_PRIVS,1,0,0,0,0)!=0||call6(SYS_seccomp,SECCOMP_SET_MODE_FILTER,SECCOMP_FILTER_FLAG_TSYNC,(long)&program,0,0,0)!=0)fail();
  if(call6(SYS_prctl,PR_GET_SECCOMP,0,0,0,0,0)!=2)fail();
  /* Invalid descriptors/pointers cannot send packets or start processes even
   * if a broken filter accidentally allows a probe. Never probe fork itself. */
  const long probes[]={SYS_socketpair,SYS_bind,SYS_connect,SYS_listen,SYS_accept,SYS_accept4,SYS_getsockname,SYS_getpeername,SYS_getsockopt,SYS_setsockopt,SYS_sendto,SYS_recvfrom,SYS_sendmsg,SYS_recvmsg,SYS_sendmmsg,SYS_recvmmsg,SYS_shutdown,SYS_execve,SYS_execveat,435,425,426,427,438};
  for(unsigned i=0;i<sizeof(probes)/sizeof(probes[0]);i++)if(call6(probes[i],-1,0,0,0,0,0)!=-1)fail();
  for(long family=2;family<=10;family+=8) {
    long fd=call6(SYS_socket,family,1,0,0,0,0);
    if(fd!=-1){if(fd>=0)call6(SYS_close,fd,0,0,0,0,0);fail();}
  }
  const char *head="GOEPAY_PHP_GUARD_V1 ";
  const char *tail=" seccomp=2 network=denied exec=denied uid=65534\n";
  const char *ready="guarded";
  for(unsigned i=0;i<7;i++)state[i]=ready[i];
  call6(SYS_write,2,(long)head,length(head),0,0,0);
  call6(SYS_write,2,(long)nonce,32,0,0,0);
  call6(SYS_write,2,(long)tail,length(tail),0,0,0);
}
